package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/biodream-llc/prova/internal/auth"
	"github.com/biodream-llc/prova/internal/authweb"
	"github.com/biodream-llc/prova/internal/oidc"
	"github.com/biodream-llc/prova/internal/saml"
	"github.com/biodream-llc/prova/internal/store"
)

// authFlags are the sign-in settings of the server.
type authFlags struct {
	dataDir   *string
	publicURL *string
	demo      *string

	oidcIssuer, oidcClientID, oidcSecretFile, oidcLabel, oidcNameClaim, oidcGroup, oidcGroupsClaim *string

	samlMetadata, samlEntityID, samlLabel, samlNameAttr, samlGroup, samlGroupsAttr *string
}

func registerAuthFlags(fs *flag.FlagSet) *authFlags {
	return &authFlags{
		dataDir:   fs.String("data", defaultDataDir(), "data directory: the database prova.db lives here"),
		publicURL: fs.String("public-url", "", "this server's public https URL, e.g. https://prova.example.com:8443 (needed for OIDC/SAML)"),
		demo: fs.String("demo-account", "", "NAME:PASSWORD - a public demonstration: a viewer account whose name and password "+
			"the sign-in screen shows (it can look at everything and change nothing)"),

		oidcIssuer:      fs.String("oidc-issuer", "", "OpenID Connect issuer URL (enables OIDC sign-in)"),
		oidcClientID:    fs.String("oidc-client-id", "", "OpenID Connect client id"),
		oidcSecretFile:  fs.String("oidc-client-secret-file", "", "file holding the OIDC client secret (or set PROVA_OIDC_CLIENT_SECRET)"),
		oidcLabel:       fs.String("oidc-label", "", "text of the OIDC sign-in button"),
		oidcNameClaim:   fs.String("oidc-name-claim", "", "ID-token claim naming the account (default preferred_username)"),
		oidcGroup:       fs.String("oidc-group", "", "group a person must be in to sign in through OIDC"),
		oidcGroupsClaim: fs.String("oidc-groups-claim", "", "claim listing groups (default groups)"),

		samlMetadata:   fs.String("saml-idp-metadata", "", "SAML identity provider metadata: an https URL or a file (enables SAML sign-in)"),
		samlEntityID:   fs.String("saml-entity-id", "", "this service provider's entity id (default PUBLIC-URL/api/auth/saml/metadata)"),
		samlLabel:      fs.String("saml-label", "", "text of the SAML sign-in button"),
		samlNameAttr:   fs.String("saml-name-attribute", "", "assertion attribute naming the account (default email, then UPN, then NameID)"),
		samlGroup:      fs.String("saml-group", "", "group a person must be in to sign in through SAML"),
		samlGroupsAttr: fs.String("saml-groups-attribute", "", "attribute listing groups (default groups)"),
	}
}

func defaultDataDir() string {
	if d := os.Getenv("PROVA_DATA"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "prova")
}

func dbPath(dataDir string) string { return filepath.Join(dataDir, "prova.db") }

// openStore opens the database in dataDir, importing the accounts of an earlier prova that kept them in users.json.
func openStore(dataDir string) (*store.Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(dbPath(dataDir))
	if err != nil {
		return nil, err
	}
	if err := importUsersJSON(st, filepath.Join(dataDir, "users.json")); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

// importUsersJSON moves accounts from the users.json file an earlier prova kept into the database, once: the file is
// renamed users.json.imported afterwards. Only an empty database is imported into.
func importUsersJSON(st *store.Store, path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if n, err := st.CountUsers(); err != nil || n > 0 {
		return err
	}
	var f struct {
		Users []struct {
			Name, Role, Salt, Hash string
			SSO                    bool
			Iterations             int
			Created                time.Time
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, u := range f.Users {
		if err := st.PutUser(store.User{Name: u.Name, Role: u.Role, SSO: u.SSO, Salt: u.Salt, Hash: u.Hash,
			Iterations: u.Iterations, Created: u.Created}, false); err != nil {
			return err
		}
	}
	log.Printf("prova: imported %d accounts from %s into the database", len(f.Users), path)
	return os.Rename(path, path+".imported")
}

// setupAuth bootstraps the first administrator, the demonstration account if asked for, and OIDC and SAML when
// configured.
func setupAuth(ctx context.Context, f *authFlags, st *store.Store) (*authweb.Server, error) {
	users := auth.NewUsers(st)
	if n, err := users.Count(); err != nil {
		return nil, err
	} else if n == 0 {
		pw, err := users.Set("admin", auth.Admin)
		if err != nil {
			return nil, fmt.Errorf("bootstrap admin: %w", err)
		}
		log.Printf("prova: ================================================================")
		log.Printf("prova: first start — created the administrator account")
		log.Printf("prova:     name:     admin")
		log.Printf("prova:     password: %s", pw)
		log.Printf("prova: shown once. Add people with `prova user add NAME ROLE`; reset with `prova user reset admin`.")
		log.Printf("prova: ================================================================")
	}
	sessions := auth.NewSessions(users)

	var demo *authweb.Demo
	if *f.demo != "" {
		name, pw, ok := strings.Cut(*f.demo, ":")
		if !ok || name == "" || pw == "" {
			return nil, errors.New("-demo-account is NAME:PASSWORD")
		}
		// Re-set only when it differs, so a restart does not end the visitors' sessions.
		if u, exists, _ := users.Get(name); !exists || u.Role != auth.Viewer || !users.HasPassword(name, pw) {
			if err := users.SetPassword(name, auth.Viewer, pw); err != nil {
				return nil, fmt.Errorf("-demo-account: %w", err)
			}
		}
		demo = &authweb.Demo{Name: name, Password: pw}
		log.Printf("prova: public demonstration: the sign-in screen offers %s (a viewer)", name)
	}

	var o *authweb.OIDC
	var s *authweb.SAML
	pub := strings.TrimSuffix(*f.publicURL, "/")
	if (*f.oidcIssuer != "" || *f.samlMetadata != "") && pub == "" {
		return nil, errors.New("single sign-on needs -public-url, the https address people open prova at")
	}
	if pub != "" {
		if u, err := url.Parse(pub); err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("-public-url must be an https URL, not %q", pub)
		}
	}

	if *f.oidcIssuer != "" {
		secret := os.Getenv("PROVA_OIDC_CLIENT_SECRET")
		if *f.oidcSecretFile != "" {
			b, err := os.ReadFile(*f.oidcSecretFile)
			if err != nil {
				return nil, fmt.Errorf("oidc client secret: %w", err)
			}
			secret = strings.TrimSpace(string(b))
		}
		dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		prov, err := oidc.Discover(dctx, oidc.Config{
			Issuer: *f.oidcIssuer, ClientID: *f.oidcClientID, ClientSecret: secret,
			RedirectURL: pub + "/api/auth/oidc/callback",
			NameClaim:   *f.oidcNameClaim, Group: *f.oidcGroup, GroupsClaim: *f.oidcGroupsClaim,
		})
		if err != nil {
			return nil, fmt.Errorf("oidc: %w", err)
		}
		o = &authweb.OIDC{Provider: prov, Label: *f.oidcLabel}
		log.Printf("prova: OpenID Connect sign-in via %s (redirect %s/api/auth/oidc/callback)", *f.oidcIssuer, pub)
	}

	if *f.samlMetadata != "" {
		doc, err := readMetadata(ctx, *f.samlMetadata)
		if err != nil {
			return nil, fmt.Errorf("saml: %w", err)
		}
		md, err := saml.ParseIDPMetadata(doc)
		if err != nil {
			return nil, fmt.Errorf("saml: %w", err)
		}
		entity := *f.samlEntityID
		if entity == "" {
			entity = pub + "/api/auth/saml/metadata"
		}
		acs := pub + "/api/auth/saml/acs"
		sp, err := saml.New(saml.Config{EntityID: entity, ACSPath: acs, IdPSSOURL: md.SSOURL, IdPCertPEM: md.CertPEM()})
		if err != nil {
			return nil, fmt.Errorf("saml: %w", err)
		}
		s = &authweb.SAML{SP: sp, EntityID: entity, ACSURL: acs, NameAttribute: *f.samlNameAttr,
			Group: *f.samlGroup, GroupsAttribute: *f.samlGroupsAttr, Label: *f.samlLabel}
		log.Printf("prova: SAML sign-in via %s (entity %s, ACS %s)", md.SSOURL, entity, acs)
	}

	srv := authweb.New(sessions, o, s, true)
	srv.Demo = demo
	srv.Activity = func(actor, action, detail string) {
		if err := st.Log(actor, action, detail); err != nil {
			log.Printf("prova: activity log: %v", err)
		}
	}
	return srv, nil
}

// readMetadata reads identity provider metadata from an https URL or a file.
func readMetadata(ctx context.Context, src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") {
		return nil, fmt.Errorf("the metadata URL must be https, not %q", src)
	}
	if strings.HasPrefix(src, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return nil, err
		}
		resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching the metadata: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("the metadata URL answered %s", resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	}
	return os.ReadFile(src)
}

// userCLI is `prova user`: manage accounts in the database. A running server sees the change at its next request.
func userCLI(args []string) int {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	dataDir := fs.String("data", defaultDataDir(), "data directory")
	usage := func() {
		fmt.Fprintln(os.Stderr, `usage: prova user [-data DIR] COMMAND
  list                    list accounts
  add   NAME ROLE         create a password account (viewer | operator | admin); prints its password once
  reset NAME              give an account a new generated password
  sso   NAME ROLE         create an account that signs in only through OIDC/SAML
  remove NAME             delete an account`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		usage()
		return 2
	}
	st, err := openStore(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prova:", err)
		return 1
	}
	defer st.Close()
	users := auth.NewUsers(st)
	audit := func(action, detail string) { _ = st.Log("(command line)", action, detail) }
	need := func(n int) bool {
		if len(rest) != n {
			usage()
			return false
		}
		return true
	}
	switch rest[0] {
	case "list":
		us, err := users.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		for _, u := range us {
			how := "password"
			if u.SSO {
				how = "single sign-on"
			}
			fmt.Printf("%-24s %-9s %s\n", u.Name, u.Role, how)
		}
	case "add":
		if !need(3) {
			return 2
		}
		pw, err := users.Set(rest[1], auth.Role(rest[2]))
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		audit("set account", rest[1]+": "+rest[2]+", new password")
		fmt.Printf("account %s (%s)\npassword: %s\n(shown once)\n", rest[1], rest[2], pw)
	case "reset":
		if !need(2) {
			return 2
		}
		u, ok, err := users.Get(rest[1])
		if err != nil || !ok {
			fmt.Fprintln(os.Stderr, "prova: no such account", rest[1])
			return 1
		}
		pw, err := users.Set(u.Name, u.Role)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		audit("set account", u.Name+": "+string(u.Role)+", new password")
		fmt.Printf("account %s (%s)\nnew password: %s\n(shown once)\n", u.Name, u.Role, pw)
	case "sso":
		if !need(3) {
			return 2
		}
		if err := users.SetSSO(rest[1], auth.Role(rest[2])); err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		audit("set account", rest[1]+": "+rest[2]+", single sign-on")
		fmt.Printf("account %s (%s) signs in through single sign-on\n", rest[1], rest[2])
	case "remove":
		if !need(2) {
			return 2
		}
		if err := users.Remove(rest[1]); err != nil {
			fmt.Fprintln(os.Stderr, "prova:", err)
			return 1
		}
		audit("removed account", rest[1])
		fmt.Printf("removed %s\n", rest[1])
	default:
		usage()
		return 2
	}
	return 0
}
