// The scope of this module is to create an issue on GitHub for each
// issue found in a Secondary Report analysis.
package feedback

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/oauth2"

	"github.com/PEDSnet/tools/cmd/dqa/results"
	"github.com/google/go-github/github"
)

const (
	repoOwner = "PEDSnet"

	dataQualityLabel        = "Data Quality"
	dataQualitySummaryLabel = "Data Quality Summary"
)

var (
	// Labels that require a string.
	dataCycleLabel = Labeler("Data Cycle")
	tableLabel     = Labeler("Table")
	rankLabel      = Labeler("Rank")
	causeLabel     = Labeler("Cause")
	statusLabel    = Labeler("Status")
)

func Labeler(p string) func(interface{}) string {
	return func(v interface{}) string {
		return fmt.Sprintf("%s: %s", p, v)
	}
}

func ParseLabel(l string) (string, string, error) {
	toks := strings.SplitN(l, ": ", 2)
	if len(toks) != 2 {
		return "", "", fmt.Errorf("Could not parse label `%s`", l)
	}

	return toks[0], toks[1], nil
}

type GithubReport struct {
	Site       string
	ETLVersion string
	DataCycle  string

	// Keep track of all results that were included in this report
	// for the summary.
	results results.Results

	client *github.Client
}

func (gr *GithubReport) Len() int {
	return len(gr.results)
}

// FetchSummaryIssues fetches the DQA summary issue.
func (gr *GithubReport) FetchSummaryIssue(ir *github.IssueRequest) (*github.Issue, error) {
	opts := &github.IssueListByRepoOptions{
		State:  "all",
		Labels: *ir.Labels,
	}

	issues, _, err := gr.client.Issues.ListByRepo(repoOwner, gr.Site, opts)
	if err != nil {
		return nil, err
	}

	if len(issues) == 1 {
		return &issues[0], nil
	}

	if len(issues) > 1 {
		// List of URLs to inspect.
		urls := make([]string, len(issues))

		for i, issue := range issues {
			urls[i] = fmt.Sprintf("- %s", issue.HTMLURL)
		}

		return nil, fmt.Errorf("Multiple issues match:\n%s", strings.Join(urls, "\n"))
	}

	return nil, nil
}

// FetchIssues fetches all issues for this site and data cyle.
func (gr *GithubReport) FetchIssues() ([]github.Issue, error) {
	labels := []string{
		dataQualityLabel,
		dataCycleLabel(gr.DataCycle),
	}

	opts := &github.IssueListByRepoOptions{
		State:  "all",
		Labels: labels,
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	var issues []github.Issue

	for {
		page, resp, err := gr.client.Issues.ListByRepo(repoOwner, gr.Site, opts)
		if err != nil {
			return nil, err
		}

		issues = append(issues, page...)

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return issues, nil
}

// BuildSummaryIssue builds a new issue requeset for the summary issue for this data cycle.
func (gr *GithubReport) BuildSummaryIssue() (*github.IssueRequest, error) {
	f := &results.File{
		Results: gr.results,
	}

	r := results.NewMarkdownReport(f)
	buf := bytes.NewBuffer(nil)

	if err := r.Render(buf); err != nil {
		return nil, err
	}

	res := f.Results[0]

	title := fmt.Sprintf("DQA Summary: %s (%s) for PEDSnet CDM v%s", gr.DataCycle, gr.ETLVersion, res.ModelVersion)
	body := buf.String()
	labels := []string{
		dataQualityLabel,
		dataQualitySummaryLabel,
		dataCycleLabel(gr.DataCycle),
	}

	ir := github.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	}

	return &ir, nil
}

// BuildIssue builds a new issue request based on the result issue.
func (gr *GithubReport) BuildIssue(r *results.Result) (*github.IssueRequest, error) {
	if r.SiteName() != gr.Site || r.ETLVersion() != gr.ETLVersion {
		return nil, fmt.Errorf("Result site or ETL version does not match reports")
	}

	title := fmt.Sprintf("DQA: %s (%s): %s/%s", gr.DataCycle, gr.ETLVersion, r.Table, r.Field)
	body := fmt.Sprintf("**Description**: %s\n**Finding**: %s", r.IssueDescription, r.Finding)

	labels := []string{
		dataQualityLabel,
		dataCycleLabel(gr.DataCycle),
		tableLabel(r.Table),
	}

	if r.Rank > 0 {
		labels = append(labels, rankLabel(r.Rank))
	}

	if r.Cause != "" {
		labels = append(labels, causeLabel(r.Cause))
	}

	if r.Status != "" {
		labels = append(labels, statusLabel(r.Status))
	}

	// All fields are pointers.
	ir := github.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	}

	gr.results = append(gr.results, r)

	return &ir, nil
}

// Ensure the minimum labels are set on the issue.
func (gr *GithubReport) EnsureLabels(num int, labels []string) ([]github.Label, error) {
	allLabels, _, err := gr.client.Issues.AddLabelsToIssue(repoOwner, gr.Site, num, labels)

	if err != nil {
		return nil, err
	}

	return allLabels, nil
}

// PostIssue sends a request to the GitHub API to create an issue.
// Upon success, a concrete issue is returned with the ID.
func (gr *GithubReport) PostIssue(ir *github.IssueRequest) (*github.Issue, error) {
	issue, _, err := gr.client.Issues.Create(repoOwner, gr.Site, ir)

	if err != nil {
		return nil, err
	}

	return issue, nil
}

// NewGitHubReport initializes a new report for posting to GitHub.
func NewGitHubReport(site, etl, cycle, token string) *GithubReport {
	tk := &oauth2.Token{
		AccessToken: token,
	}
	ts := oauth2.StaticTokenSource(tk)
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	client := github.NewClient(tc)

	return &GithubReport{
		Site:       site,
		ETLVersion: etl,
		DataCycle:  cycle,
		client:     client,
	}
}