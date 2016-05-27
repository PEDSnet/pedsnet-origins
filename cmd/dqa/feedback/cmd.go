package feedback

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PEDSnet/tools/cmd/dqa/results"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var Cmd = &cobra.Command{
	Use: "feedback",

	Short: "Top-level command for feedback subcommands.",

	Example: `pedsnet-dqa feedback generate [...]`,
}

var SyncCmd = &cobra.Command{
	Use: "sync <path>",

	Short: "Syncs Cause and Status labels from GitHub to the local CSV files.",

	Example: `pedsnet-dqa feedback sync --token=abc123 --cycle="April 2016"  SecondaryReports/CHOP/ETLv8`,

	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			cmd.Usage()
			os.Exit(0)
		}

		token := viper.GetString("feedback.token")
		dataCycle := viper.GetString("feedback.cycle")

		if dataCycle == "" {
			cmd.Println("The data cycle could not be detected. Please supply it using the --cycle option.")
			os.Exit(1)
		}

		if token == "" {
			cmd.Println("A token is required to access GitHub.")
			os.Exit(1)
		}

		dir := args[0]
		files, err := results.ReadFromDir(dir)
		if err != nil {
			cmd.Printf("Error reading files in '%s'\n", err)
			os.Exit(1)
		}

		gr := NewGitHubReport("", "", dataCycle, token)

		issuesById := make(map[int]github.Issue)

		// Iterate over each file and incrementally post the issues.
		for name, file := range files {
			var causeChanges, statusChanges int

			for _, result := range file.Results {
				if gr.Site == "" {
					// This is a bit weird, but the site and ETL version are set using the result.
					gr.Site = result.SiteName()
					gr.ETLVersion = result.ETLVersion()

					issues, err := gr.FetchIssues()
					if err != nil {
						cmd.Printf("Error fetching issues: %s\n", err)
						os.Exit(1)
					}

					for _, issue := range issues {
						issuesById[*issue.Number] = issue
					}

					cmd.Printf("Fetched %d issues.\n", len(issuesById))
				}

				if result.GithubID == "" {
					continue
				}

				id, err := strconv.Atoi(result.GithubID)
				if err != nil {
					cmd.Printf("Invalid GitHub ID: `%s`\n", result.GithubID)
					os.Exit(1)
				}

				issue, ok := issuesById[id]
				if !ok {
					cmd.Printf("GitHub issue %d exists, but result does not?\n", id)
					os.Exit(1)
				}

				var status, cause string

				for _, label := range issue.Labels {
					kind, value, err := ParseLabel(*label.Name)
					if err != nil {
						continue
					}

					switch strings.ToLower(kind) {
					case "status":
						if status != "" {
							cmd.Printf("Duplicate Status label on issue %s. Remove it and re-run.\n", *issue.HTMLURL)
							os.Exit(1)
						}

						status = value

					case "cause":
						if cause != "" {
							cmd.Printf("Duplicate Cause label on issue %s. Remove it and re-run.\n", *issue.HTMLURL)
							os.Exit(1)
						}

						cause = value
					}
				}

				if cause != result.Cause {
					fmt.Printf("Changing %s cause %s -> %s\n", result, result.Cause, cause)
					result.Cause = cause
					causeChanges++
				}

				if status != result.Status {
					fmt.Printf("Changing %s status %s -> %s\n", result, result.Status, status)
					result.Status = status
					statusChanges++
				}
			}

			// Nothing to do.
			if causeChanges == 0 && statusChanges == 0 {
				cmd.Printf("No changes to sync for '%s'.\n", name)
				continue
			}

			// File opened successfully.
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				cmd.Printf("Error opening file to write issue IDs: %s\n", err)
				os.Exit(1)
			}
			defer f.Close()
			w := results.NewWriter(f)

			if err := w.WriteAll(file.Results); err != nil {
				cmd.Printf("Error writing results to file.")
				os.Exit(1)
			}

			if err := w.Flush(); err != nil {
				cmd.Printf("Error flushing results to file.")
				os.Exit(1)
			}

			cmd.Printf("Synced labels to '%s'.\n", name)
		}
	},
}

var GenerateCmd = &cobra.Command{
	Use: "generate <path>",

	Short: "Generates and posts a set of issues to GitHub.",

	Example: `pedsnet-dqa feedback generate --post --token=abc123 --cycle="April 2016" SecondaryReports/CHOP/ETLv8`,

	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			cmd.Usage()
			os.Exit(0)
		}

		token := viper.GetString("feedback.token")
		dataCycle := viper.GetString("feedback.cycle")
		post := viper.GetBool("feedback.generate.post")
		printSummary := viper.GetBool("feedback.generate.print-summary")

		if dataCycle == "" {
			cmd.Println("The data cycle could not be detected. Please supply it using the --cycle option.")
			os.Exit(1)
		}

		if post && token == "" {
			cmd.Println("A token is required to post issues to GitHub.")
			os.Exit(1)
		}

		dir := args[0]
		files, err := results.ReadFromDir(dir)
		if err != nil {
			cmd.Printf("Error reading files in '%s'\n", err)
			os.Exit(1)
		}

		gr := NewGitHubReport("", "", dataCycle, token)

		// Iterate over each file and incrementally post the issues.
		for name, file := range files {
			var newIssues results.Results

			for _, result := range file.Results {

				// This is a bit weird, but the site and ETL version are set using the result.
				if gr.Site == "" {
					gr.Site = result.SiteName()
					gr.ETLVersion = result.ETLVersion()
				}

				// Not in an issue. This will not be included in the summary report.
				if !result.IsIssue() {
					continue
				}

				newIssues = append(newIssues, result)

				ir, err := gr.BuildIssue(result)
				if err != nil {
					cmd.Printf("Error building issue request: %s\n", err)
					os.Exit(1)
				}

				// Only post if it does not already have a GitHub ID.
				if post {
					if result.GithubID == "" {
						issue, err := gr.PostIssue(ir)
						if err != nil {
							cmd.Printf("Error posting issue to GitHub: %s\n", err)
							continue
						}

						result.GithubID = fmt.Sprintf("%d", *issue.Number)
					} else {
						num, err := strconv.Atoi(result.GithubID)
						if err != nil {
							cmd.Printf("Error converting GithubID %s to integer: %s", result.GithubID, err)
							continue
						}

						_, err = gr.EnsureLabels(num, *ir.Labels)
						if err != nil {
							cmd.Printf("Error setting labels on issue #%s\n: %s", num, err)
							continue
						}
					}
				}
			}

			if len(newIssues) == 0 {
				cmd.Printf("No new issues for '%s'\n", name)
				continue
			}

			cmd.Printf("%d issues found in '%s'\n", len(newIssues), name)

			//
			if post {
				success := true
				f, err := os.Create(filepath.Join(dir, name))

				// File opened successfully.
				if err == nil {
					defer f.Close()
					w := results.NewWriter(f)

					if err := w.WriteAll(file.Results); err != nil {
						success = false
						cmd.Printf("Error writing results to file.")
					}

					if err := w.Flush(); err != nil {
						success = false
						cmd.Printf("Error flushing results to file.")
					}

					cmd.Printf("Saved new issue IDs to '%s'\n", name)
				} else {
					success = false
					cmd.Printf("Error opening file to write issue IDs: %s\n", err)
				}

				// Fallback to writing to standard out.
				if !success {
					cmd.Printf("Falling back to printing the results so they can be copy and pasted into '%s'.", name)
					// Only print the new issues to stdout.
					w := results.NewWriter(os.Stdout)
					w.WriteAll(newIssues)
					w.Flush()
					continue
				}
			}
		}

		if gr.Len() == 0 {
			cmd.Println("No issues to report.")
			return
		}

		// Build the summary issue.
		ir, err := gr.BuildSummaryIssue()

		if err != nil {
			cmd.Printf("Error building summary issue: %s\n", err)
			cmd.Println("Note: This can be safely retried without duplicating issues.")
			os.Exit(1)
		}

		// Check if a summary issue already exists for this site + data cycle.
		issue, err := gr.FetchSummaryIssue(ir)
		if err != nil {
			cmd.Printf("Error fetching summary issue from GitHub: %s\n", err)
			os.Exit(1)
		}

		// No summary issue found.
		if issue == nil {
			cmd.Printf("No summary issue found.")
		} else {
			cmd.Printf("Summary issue already exists: %s\n", *issue.HTMLURL)
		}

		if !post || printSummary {
			fmt.Println(*ir.Body)
		} else if issue == nil {
			issue, err := gr.PostIssue(ir)
			if err != nil {
				cmd.Printf("Error posting summary issue to GitHub: %s\n", err)
				cmd.Println("Note: This can be safely retried without duplicating issues.")
				os.Exit(1)
			}

			cmd.Printf("Summary issue URL: %s\n", *issue.HTMLURL)
		}
	},
}

func init() {
	Cmd.AddCommand(GenerateCmd)
	Cmd.AddCommand(SyncCmd)

	pflags := Cmd.PersistentFlags()

	pflags.String("token", "", "Token used to authenticate with GitHub.")
	pflags.String("cycle", "", "The data cycle for this report.")

	viper.BindPFlag("feedback.cycle", pflags.Lookup("cycle"))
	viper.BindPFlag("feedback.token", pflags.Lookup("token"))

	// Generate flags.
	gflags := GenerateCmd.Flags()

	gflags.Bool("post", false, "Posts the issues to GitHub.")
	gflags.Bool("print-summary", false, "Print the summary to stdout rather than posting it.")

	viper.BindPFlag("feedback.generate.post", gflags.Lookup("post"))
	viper.BindPFlag("feedback.generate.print-summary", gflags.Lookup("print-summary"))
}