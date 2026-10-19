package validate

import "encoding/json"

// SARIF renders a validation report as SARIF 2.1.0, the Static Analysis Results Interchange Format
// that GitHub code scanning and editors consume. Each finding becomes a result keyed by its rule.
func (r Report) SARIF() ([]byte, error) {
	rulesSeen := map[string]bool{}
	var rules []sarifRule
	var results []sarifResult
	for _, f := range r.Findings {
		if !rulesSeen[f.Rule] {
			rulesSeen[f.Rule] = true
			rules = append(rules, sarifRule{ID: f.Rule, Name: f.Rule,
				ShortDescription: sarifText{Text: f.Rule}})
		}
		msg := f.Message
		if f.Fix != "" {
			msg += " — " + f.Fix
		}
		results = append(results, sarifResult{
			RuleID:  f.Rule,
			Level:   string(f.Severity),
			Message: sarifText{Text: msg},
			Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: locURI(r.Kind)},
				Region:           sarifRegion{StartLine: 1, Snippet: sarifText{Text: f.Path}},
			}}},
		})
	}
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "prova",
				InformationURI: "https://github.com/BioDream-LLC/prova",
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	return json.MarshalIndent(log, "", "  ")
}

func locURI(kind string) string {
	if kind == "" {
		return "message.xml"
	}
	return kind + ".xml"
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ShortDescription sarifText `json:"shortDescription"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int       `json:"startLine"`
	Snippet   sarifText `json:"snippet"`
}
