// prova-proof — the companion prover for prova (a BioDream LLC product).
//
// It reads an abstract argumentation framework on stdin, independently recomputes the grounded
// labelling, verifies that labelling is a complete extension, and decides stable-semantics questions
// about the query argument: whether a stable extension exists, and whether the query is skeptically
// (in every stable extension) and credulously (in some) accepted. The result is a certificate prova
// attaches to the verdict — a second, independent check of the same framework, not an AI explanation.
//
// The satisfiability search can run on either of two backends, selected at build time:
//
//   * CaDiCaL (Armin Biere's CDCL solver), when built with -DPROVA_USE_CADICAL. This is the same
//     solver quorum cross-checks its reasoning with. The engine reports itself as "prova-proof+cadical".
//   * a small self-contained DPLL otherwise, so the companion builds with no third-party dependency
//     and prova stays a single self-sufficient binary. The engine reports itself as "prova-proof".
//
// The CNF encoding below is the SEARCH-independent part: it is transcribed from the semantics, and a
// bug in it would almost certainly disagree with prova's own Go result rather than silently agree,
// because the two arrive at an answer by completely different routes.
//
// Protocol (line-based, no JSON dependency):
//   stdin : ARG <id>\n ... ATK <from> <to>\n ... QUERY <id>\n END\n
//   stdout: ENGINE <name>\n QUERY_GROUNDED <label>\n COMPLETE <0|1>\n
//           STABLE_EXISTS <0|1>\n SKEPTICAL <0|1>\n CREDULOUS <0|1>\n OK\n

#include <iostream>
#include <sstream>
#include <string>
#include <unordered_map>
#include <vector>

#ifdef PROVA_USE_CADICAL
#include "cadical.hpp"
#endif

namespace {

enum Label { UNDEC = 0, IN = 1, OUT = 2 };

// Sat is the satisfiability backend. A clause database is built once from the stable-semantics
// encoding; each question is then a solve under at most one assumption about the query variable.
// Both backends answer the same CNF; only the search differs.
class Sat {
public:
  explicit Sat(int nvars) : n_(nvars) {}

  // add appends a clause (a list of signed, one-based literals).
  void add(const std::vector<int> &clause) { clauses_.push_back(clause); }

  // satisfiable returns true if the clause set is satisfiable; if assume != 0 that literal is
  // additionally assumed for this one question.
  bool satisfiable(int assume) const;

  const char *engine() const;

private:
  int n_;
  std::vector<std::vector<int>> clauses_;
};

#ifdef PROVA_USE_CADICAL

bool Sat::satisfiable(int assume) const {
  CaDiCaL::Solver s;
  for (const auto &cl : clauses_) {
    for (int lit : cl)
      s.add(lit);
    s.add(0);
  }
  if (assume)
    s.assume(assume);
  return s.solve() == 10; // 10 = SATISFIABLE
}

const char *Sat::engine() const { return "prova-proof+cadical"; }

#else

// DPLL over integer literals (+v / -v), variables 1..n, with unit propagation and backtracking.
bool dpll(const std::vector<std::vector<int>> &clauses, std::vector<int> val, int n) {
  bool changed = true;
  while (changed) {
    changed = false;
    for (const auto &cl : clauses) {
      bool satisfied = false;
      int unassigned = 0, count = 0;
      for (int lit : cl) {
        int v = lit < 0 ? -lit : lit;
        int sign = lit > 0 ? 1 : -1;
        if (val[v] == sign) {
          satisfied = true;
          break;
        }
        if (val[v] == 0) {
          ++count;
          unassigned = lit;
        }
      }
      if (satisfied)
        continue;
      if (count == 0)
        return false; // conflict
      if (count == 1) {
        int v = unassigned < 0 ? -unassigned : unassigned;
        val[v] = unassigned > 0 ? 1 : -1;
        changed = true;
      }
    }
  }
  int pick = 0;
  for (int i = 1; i <= n; ++i)
    if (val[i] == 0) {
      pick = i;
      break;
    }
  if (pick == 0)
    return true; // fully assigned, no conflict
  std::vector<int> a = val;
  a[pick] = 1;
  if (dpll(clauses, a, n))
    return true;
  std::vector<int> b = val;
  b[pick] = -1;
  return dpll(clauses, b, n);
}

bool Sat::satisfiable(int assume) const {
  std::vector<std::vector<int>> cl = clauses_;
  if (assume)
    cl.push_back({assume});
  return dpll(cl, std::vector<int>(n_ + 1, 0), n_);
}

const char *Sat::engine() const { return "prova-proof"; }

#endif

} // namespace

int main() {
  std::vector<std::string> ids;
  std::unordered_map<std::string, int> index;
  std::vector<std::pair<int, int>> attacks; // (from, to) by index
  std::string query;

  auto idOf = [&](const std::string &s) -> int {
    auto it = index.find(s);
    if (it != index.end())
      return it->second;
    int i = static_cast<int>(ids.size());
    index[s] = i;
    ids.push_back(s);
    return i;
  };

  std::string line;
  while (std::getline(std::cin, line)) {
    std::istringstream ss(line);
    std::string op;
    ss >> op;
    if (op == "ARG") {
      std::string id;
      ss >> id;
      if (!id.empty())
        idOf(id);
    } else if (op == "ATK") {
      std::string f, t;
      ss >> f >> t;
      if (!f.empty() && !t.empty())
        attacks.emplace_back(idOf(f), idOf(t));
    } else if (op == "QUERY") {
      ss >> query;
    } else if (op == "END") {
      break;
    }
  }

  int n = static_cast<int>(ids.size());
  std::vector<std::vector<int>> attackers(n);
  for (auto &a : attacks)
    attackers[a.second].push_back(a.first);

  // Grounded labelling by least-fixed-point iteration.
  std::vector<Label> label(n, UNDEC);
  for (bool changed = true; changed;) {
    changed = false;
    for (int i = 0; i < n; ++i) {
      if (label[i] != UNDEC)
        continue;
      bool allOut = true, anyIn = false;
      for (int b : attackers[i]) {
        if (label[b] == IN) {
          anyIn = true;
          allOut = false;
        } else if (label[b] == UNDEC) {
          allOut = false;
        }
      }
      if (allOut) {
        label[i] = IN;
        changed = true;
      } else if (anyIn) {
        label[i] = OUT;
        changed = true;
      }
    }
  }

  // Verify the grounded labelling is a complete labelling.
  bool complete = true;
  for (int i = 0; i < n && complete; ++i) {
    bool allOut = true, anyIn = false;
    for (int b : attackers[i]) {
      if (label[b] == IN) {
        anyIn = true;
        allOut = false;
      } else if (label[b] == UNDEC) {
        allOut = false;
      }
    }
    if (label[i] == IN && !allOut)
      complete = false;
    if (label[i] == OUT && !anyIn)
      complete = false;
    if (label[i] == UNDEC && (allOut || anyIn))
      complete = false;
  }

  // Stable semantics as SAT: variable (i+1) means argument i is in the extension.
  //   no attackers      -> unit (x_i)
  //   attackers B       -> (x_i OR x_b for b in B)  and  (!x_i OR !x_b) for each b in B
  Sat sat(n);
  for (int i = 0; i < n; ++i) {
    int xi = i + 1;
    if (attackers[i].empty()) {
      sat.add({xi});
      continue;
    }
    std::vector<int> orClause{xi};
    for (int b : attackers[i]) {
      orClause.push_back(b + 1);
      sat.add({-xi, -(b + 1)});
    }
    sat.add(orClause);
  }

  bool stableExists = sat.satisfiable(0);

  bool skeptical = false, credulous = false;
  auto qi = index.find(query);
  if (qi != index.end()) {
    int qv = qi->second + 1;
    credulous = sat.satisfiable(qv);        // some stable extension contains the query
    bool canBeOut = sat.satisfiable(-qv);   // some stable extension excludes it
    skeptical = stableExists && !canBeOut;  // in every stable extension
  }

  // Independently cross-check the grounded label of the query via complete-extension semantics (the
  // encoding is Dung's characteristic function): an IN query is in every complete extension (so
  // excluding it is UNSAT), an OUT query in none (including it is UNSAT), an UNDEC query in some and
  // absent from another (both SAT).
  bool groundedVerified = false;
  if (qi != index.end()) {
    Sat ce(2 * n);
    auto in_var = [](int a) { return a + 1; };
    auto att_var = [n](int a) { return n + a + 1; };
    for (auto &e : attacks)
      ce.add({-in_var(e.first), -in_var(e.second)}); // conflict-free
    for (int a = 0; a < n; ++a) {                     // att[a] <-> OR over attackers of a
      if (attackers[a].empty()) {
        ce.add({-att_var(a)});
        continue;
      }
      std::vector<int> orC{-att_var(a)};
      for (int c : attackers[a])
        orC.push_back(in_var(c));
      ce.add(orC);
      for (int c : attackers[a])
        ce.add({-in_var(c), att_var(a)});
    }
    for (int b = 0; b < n; ++b) { // in[b] <-> every attacker of b is attacked
      if (attackers[b].empty()) {
        ce.add({in_var(b)});
        continue;
      }
      for (int a : attackers[b])
        ce.add({-in_var(b), att_var(a)});
      std::vector<int> c{in_var(b)};
      for (int a : attackers[b])
        c.push_back(-att_var(a));
      ce.add(c);
    }
    int qiv = in_var(qi->second);
    bool canIn = ce.satisfiable(qiv);
    bool canOut = ce.satisfiable(-qiv);
    switch (label[qi->second]) {
    case IN:
      groundedVerified = !canOut;
      break;
    case OUT:
      groundedVerified = !canIn;
      break;
    default:
      groundedVerified = canIn && canOut;
      break;
    }
  }

  const char *qlabel = "undec";
  if (qi != index.end()) {
    switch (label[qi->second]) {
    case IN:
      qlabel = "in";
      break;
    case OUT:
      qlabel = "out";
      break;
    default:
      qlabel = "undec";
      break;
    }
  }

  std::cout << "ENGINE " << sat.engine() << "\n";
  std::cout << "QUERY_GROUNDED " << qlabel << "\n";
  std::cout << "COMPLETE " << (complete ? 1 : 0) << "\n";
  std::cout << "GROUNDED_VERIFIED " << (groundedVerified ? 1 : 0) << "\n";
  std::cout << "STABLE_EXISTS " << (stableExists ? 1 : 0) << "\n";
  std::cout << "SKEPTICAL " << (skeptical ? 1 : 0) << "\n";
  std::cout << "CREDULOUS " << (credulous ? 1 : 0) << "\n";
  std::cout << "OK\n";
  return 0;
}
