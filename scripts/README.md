# Weekly Summary Generator for KubeRay

This directory contains tools for generating weekly summaries of GitHub activity in the KubeRay repository.

## Files

- `weekly-summary.py` - Python script to generate weekly summaries of issues and pull requests
- `changelog-generator.py` - Existing script for generating changelogs from commit history

## Weekly Summary Generator

### Purpose
The `weekly-summary.py` script automatically generates comprehensive summaries of new issues and pull requests in the ray-project/kuberay repository. This helps maintainers and community members stay updated on recent activity.

### Features
- Fetches all issues and PRs created since a specified date
- Categorizes issues by type (Bug Reports, Feature Requests, etc.)
- Categorizes PRs by type (Bug Fixes, New Features, Refactoring, etc.)
- Provides contributor statistics
- Generates markdown-formatted output suitable for documentation

### Prerequisites
```bash
pip install PyGithub
```

### Usage

#### Basic Usage (last 7 days)
```bash
python scripts/weekly-summary.py --token YOUR_GITHUB_TOKEN
```

#### Custom Date Range
```bash
python scripts/weekly-summary.py --since 2025-08-24 --token YOUR_GITHUB_TOKEN
```

#### Using Environment Variable for Token
```bash
export GITHUB_TOKEN=your_token_here
python scripts/weekly-summary.py --since 2025-08-24
```

### GitHub Token Setup

1. Go to GitHub Settings → Developer settings → Personal access tokens
2. Generate a new token with the following permissions:
   - `public_repo` (for accessing public repository data)
   - `read:user` (for user information)
3. Use the token with the script or set it as an environment variable

### Output Format

The script generates a markdown report with:

- **Summary Statistics**: Count of new issues and PRs
- **Categorized Issues**: Issues grouped by type with labels and authors
- **Categorized Pull Requests**: PRs grouped by type with status indicators
- **Top Contributors**: Ranked list of most active contributors

### Issue Categories
- Bug Reports
- Feature Requests  
- Good First Issues
- Help Wanted
- Documentation
- Refactoring
- Component Specific
- General Issues

### PR Categories
- Bug Fixes
- New Features
- Refactoring
- Maintenance
- Testing
- Documentation
- Dependencies
- General Changes

### Status Indicators
- 🟢 Open
- 🟣 Merged
- 🔴 Closed

### Example Output

See [WEEKLY_SUMMARY_2025_08_24.md](../WEEKLY_SUMMARY_2025_08_24.md) for an example of generated output.

### Automation

This script can be easily integrated into CI/CD workflows:

```yaml
# Example GitHub Action
name: Weekly Summary
on:
  schedule:
    - cron: '0 9 * * MON'  # Every Monday at 9 AM UTC
  workflow_dispatch:

jobs:
  summary:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Setup Python
        uses: actions/setup-python@v4
        with:
          python-version: '3.x'
      - name: Install dependencies
        run: pip install PyGithub
      - name: Generate Summary
        run: python scripts/weekly-summary.py --token ${{ secrets.GITHUB_TOKEN }}
      - name: Create Issue
        uses: peter-evans/create-issue-from-file@v4
        with:
          title: "Weekly Summary - $(date +'%Y-%m-%d')"
          content-filepath: weekly_summary.md
```

### Contributing

To improve the summary generator:

1. **Enhance Categorization**: Update the `categorize_issue()` and `categorize_pr()` functions to better classify items
2. **Add New Metrics**: Extend the summary with additional statistics or insights
3. **Improve Formatting**: Enhance the markdown output for better readability
4. **Add Filters**: Implement filters for specific components, labels, or authors

### Related Tools

- `changelog-generator.py`: For generating release changelogs from git commit history
- GitHub CLI: `gh issue list` and `gh pr list` for quick command-line queries
- GitHub API: Direct access to repository data for custom analysis

### Best Practices

1. **Regular Generation**: Run weekly summaries consistently to track project trends
2. **Archive Summaries**: Keep historical summaries for trend analysis
3. **Community Sharing**: Share summaries in community channels or documentation
4. **Feedback Integration**: Use summary insights to guide project planning and priority setting

### Troubleshooting

**Error: "No module named 'github'"**
```bash
pip install PyGithub
```

**Error: "GitHub token is required"**
- Ensure your token is valid and has the required permissions
- Set the GITHUB_TOKEN environment variable or use --token flag

**Rate Limiting Issues**
- GitHub API has rate limits (5000 requests/hour for authenticated users)
- The script makes minimal API calls, but for large date ranges, you might hit limits
- Consider using smaller date ranges or adding delays between requests