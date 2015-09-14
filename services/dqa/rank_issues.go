package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var rankIssuesCmd = &cobra.Command{
	Use: "assign-rank-to-issues <path>",

	Short: "Assigns ranks to detected issues in DQA analysis results.",

	Example: `
  pedsnet-dqa assign-rank-to-issues SecondaryReports/CHOP/ETLv4`,

	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			cmd.Usage()
			return
		}

		// Ensure this is a directory.
		fns, err := ioutil.ReadDir(args[0])

		if err != nil {
			log.Fatal(err)
		}

		var (
			path string
			f    *os.File
		)

		// Iterate over each CSV file in the directory.
		for _, fi := range fns {
			if fi.IsDir() {
				continue
			}

			path = filepath.Join(args[0], fi.Name())

			if f, err = os.Open(path); err != nil {
				log.Printf("error opening file: %s", err)
				continue
			}

			report := &Report{}

			_, err := report.ReadResults(f)

			f.Close()

			// Presumably not a valid file.
			if err != nil {
				log.Printf("error reading results: %s", err)
				continue
			}

			changed := false

			for i, r := range report.Results {
				if ruleset, rank, ok := RunRules(r); ok {
					fmt.Printf("Rule matched:\n- scope: %s\n- line: %d\n- table: %s\n- field: %s\n- issue code: %s\n- prevalence: %s\n- rank: %s\n", ruleset, i+1, r.Table, r.Field, r.IssueCode, r.Prevalence, rank)

					if r.Rank == rank {
						fmt.Println("- action: nothing (already set)")
					} else {
						fmt.Printf("- action: set rank to %s (from '%s')\n", rank, r.Rank)
						r.Rank = rank
						changed = true
					}

					fmt.Print("\n")
				}
			}

			if changed {
				// Open the save file for writing.
				if f, err = os.Create(path); err != nil {
					log.Printf("error opening file: %s", err)
					continue
				}

				rw := NewResultsWriter(f)

				for _, r := range report.Results {
					if err = rw.Write(r); err != nil {
						log.Printf("error writing to file: %s", err)
						break
					}
				}

				if err = rw.Flush(); err != nil {
					log.Printf("error flushing file: %s", err)
				}

				f.Close()
			}
		}
	},
}

// RunRules iterates through all rules for the result until a match is found.
func RunRules(r *Result) (*RuleSet, Rank, bool) {
	if rank, ok := AdminRules.Matches(r); ok {
		return AdminRules, rank, true
	}

	if rank, ok := DemographicRules.Matches(r); ok {
		return DemographicRules, rank, true
	}

	if rank, ok := FactRules.Matches(r); ok {
		return FactRules, rank, true
	}

	return nil, 0, false
}

func inSlice(s string, a []string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}

	return false
}

type Matcher interface {
	Matches(r *Result) (Rank, bool)
}

type Condition func(r *Result) bool

type Rule struct {
	Conditions []Condition
	Map        map[[2]string]Rank
}

func (r *Rule) Matches(s *Result) (Rank, bool) {
	for _, m := range r.Conditions {
		if !m(s) {
			return 0, false
		}
	}

	// Get the rank based on the issue code and prevalence.
	rank, ok := r.Map[[2]string{strings.ToLower(s.IssueCode), strings.ToLower(s.Prevalence)}]

	return rank, ok
}

// Field conditionals.
func isPersistent(r *Result) bool {
	return strings.ToLower(r.Status) == "persistent"
}

func isPrimaryKey(r *Result) bool {
	return r.Field == fmt.Sprintf("%s_id", r.Table)
}

func isSourceValue(r *Result) bool {
	return strings.HasSuffix(r.Field, "_source_value")
}

func isConceptId(r *Result) bool {
	return strings.HasSuffix(r.Field, "_concept_id")
}

func isForeignKey(r *Result) bool {
	return !isPrimaryKey(r) && strings.HasSuffix(r.Field, "_id") && !isConceptId(r)
}

func isDateYear(r *Result) bool {
	return strings.Contains(r.Field, "date") || strings.Contains(r.Field, "year")
}

func isOther(r *Result) bool {
	return !isPrimaryKey(r) && !isForeignKey(r) && !isSourceValue(r) && !isConceptId(r) && !isDateYear(r)
}

type RuleSet struct {
	Name   string
	Tables []string
	Rules  []*Rule
}

func (rs *RuleSet) String() string {
	return rs.Name
}

func (rs *RuleSet) Matches(r *Result) (Rank, bool) {
	// Global rule.
	if isPersistent(r) {
		return 0, false
	}

	if !inSlice(r.Table, rs.Tables) {
		return 0, false
	}

	for _, rule := range rs.Rules {
		if rank, ok := rule.Matches(r); ok {
			return rank, true
		}
	}

	return 0, false
}

var AdminRules = &RuleSet{
	Name: "Administrative",

	Tables: []string{
		"care_site",
		"location",
		"provider",
	},

	Rules: []*Rule{
		// Admin rules
		{
			Conditions: []Condition{
				isPrimaryKey,
			},
			Map: map[[2]string]Rank{
				{"g2-013", "high"}:   MediumRank,
				{"g2-013", "medium"}: LowRank,
				{"g2-013", "low"}:    LowRank,
			},
		},

		{
			Conditions: []Condition{
				isSourceValue,
			},
			Map: map[[2]string]Rank{
				{"g2-011", "full"}:   MediumRank,
				{"g2-011", "medium"}: LowRank,
				{"g4-002", "full"}:   MediumRank,
				{"g4-002", "high"}:   MediumRank,
				{"g4-002", "medium"}: MediumRank,
				{"g4-002", "low"}:    LowRank,
			},
		},

		{
			Conditions: []Condition{
				isConceptId,
			},
			Map: map[[2]string]Rank{
				{"g1-002", "high"}:   HighRank,
				{"g1-002", "medium"}: HighRank,
			},
		},

		{
			Conditions: []Condition{
				isForeignKey,
			},
			Map: map[[2]string]Rank{
				{"g2-013", "high"}:   MediumRank,
				{"g2-013", "medium"}: LowRank,
				{"g2-013", "low"}:    LowRank,
				{"g4-002", "full"}:   MediumRank,
			},
		},

		{
			Conditions: []Condition{
				isOther,
			},
			Map: map[[2]string]Rank{
				{"g2-011", "low"}:    LowRank,
				{"g4-002", "full"}:   MediumRank,
				{"g4-002", "high"}:   MediumRank,
				{"g4-002", "medium"}: MediumRank,
				{"g4-002", "low"}:    LowRank,
			},
		},
	},
}

var DemographicRules = &RuleSet{
	Name: "Demographic",

	Tables: []string{
		"person",
		"death",
		"observation_period",
	},

	Rules: []*Rule{
		// Demographic rules
		{
			Conditions: []Condition{
				isPrimaryKey,
			},
			Map: map[[2]string]Rank{
				{"g4-001", "high"}:   HighRank,
				{"g1-003", "low"}:    MediumRank,
				{"g2-013", "medium"}: HighRank,
			},
		},

		{
			Conditions: []Condition{
				isSourceValue,
			},
			Map: map[[2]string]Rank{
				{"g4-002", "full"}: MediumRank,
				{"g4-002", "high"}: MediumRank,
			},
		},

		{
			Conditions: []Condition{
				isForeignKey,
			},
			Map: map[[2]string]Rank{
				{"g1-003", "low"}:     MediumRank,
				{"g2-013", "medium"}:  HighRank,
				{"g2-013", "low"}:     MediumRank,
				{"g2-005", "high"}:    LowRank,
				{"g3-002", "unknown"}: MediumRank,
			},
		},

		{
			Conditions: []Condition{
				isOther,
			},
			Map: map[[2]string]Rank{
				{"g2-011", "low"}:  MediumRank,
				{"g4-002", "full"}: HighRank,
			},
		},

		{
			Conditions: []Condition{
				isConceptId,
			},
			Map: map[[2]string]Rank{
				{"g4-002", "full"}:    HighRank,
				{"g2-006", "unknown"}: HighRank,
			},
		},

		{
			Conditions: []Condition{
				isDateYear,
			},
			Map: map[[2]string]Rank{
				{"g2-009", "low"}: MediumRank,
				{"g2-010", "low"}: MediumRank,
			},
		},

		// Fact rules
		{
			Conditions: []Condition{
				isPrimaryKey,
			},
			Map: map[[2]string]Rank{
				{"g4-001", "full"}: HighRank,
			},
		},

		{
			Conditions: []Condition{
				isSourceValue,
			},
			Map: map[[2]string]Rank{
				{"g2-011", "full"}: HighRank,
				{"g4-002", "full"}: HighRank,
			},
		},
	},
}

var FactRules = &RuleSet{
	Name: "Fact",

	Tables: []string{
		"conditin_occurrence",
		"drug_exposure",
		"fact_relationship",
		"measurement",
		"observation",
		"procedure",
		"visit_occurrence",
		"visit_payer",
	},

	Rules: []*Rule{
		// Custom match.
		{
			Conditions: []Condition{
				func(r *Result) bool {
					return inSlice(r.Field, []string{
						"provider_id",
						"care_site",
					})
				},
			},
			Map: map[[2]string]Rank{
				{"g2-013", "low"}:  MediumRank,
				{"g4-002", "low"}:  LowRank,
				{"g2-005", "high"}: LowRank,
			},
		},

		{
			Conditions: []Condition{
				func(r *Result) bool {
					return inSlice(r.Field, []string{
						"person_id",
						"visit_occurrence_id",
					})
				},
			},
			Map: map[[2]string]Rank{
				{"g2-013", "high"}:   HighRank,
				{"g2-005", "high"}:   MediumRank,
				{"g2-005", "medium"}: MediumRank,
			},
		},

		{
			Conditions: []Condition{
				isOther,
			},
			Map: map[[2]string]Rank{
				{"g2-013", "high"}:    LowRank,
				{"g2-011", "high"}:    HighRank,
				{"g4-002", "high"}:    HighRank,
				{"g2-001", "unknown"}: LowRank,
				{"g2-007", "high"}:    LowRank,
				{"g2-007", "medium"}:  LowRank,
			},
		},

		{
			Conditions: []Condition{
				isConceptId,
			},
			Map: map[[2]string]Rank{
				{"g4-001", "unknown"}: HighRank,
				{"g2-012", "high"}:    MediumRank,
				{"g2-013", "high"}:    HighRank,
				{"g1-001", "full"}:    HighRank,
				{"g4-002", "full"}:    HighRank,
				{"g1-002", "high"}:    HighRank,
				{"g2-006", "low"}:     MediumRank,
			},
		},

		{
			Conditions: []Condition{
				isDateYear,
			},
			Map: map[[2]string]Rank{
				{"g2-009", "low"}:     MediumRank,
				{"g2-008", "unknown"}: MediumRank,
				{"g2-010", "low"}:     LowRank,
			},
		},
	},
}