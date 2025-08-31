#!/usr/bin/env python3
"""
Weekly Summary Generator for KubeRay Repository

This script generates a summary of new issues and pull requests 
in the ray-project/kuberay repository since a specified date.

Usage:
    python weekly-summary.py [--since YYYY-MM-DD] [--token YOUR_GITHUB_TOKEN]

If --since is not provided, it defaults to 7 days ago.
If --token is not provided, it tries to use the GITHUB_TOKEN environment variable.
"""

import argparse
import os
import sys
from datetime import datetime, timedelta
from collections import defaultdict
from github import Github

def parse_args():
    parser = argparse.ArgumentParser(description='Generate weekly summary of KubeRay issues and PRs')
    parser.add_argument('--since', type=str, 
                        help='Start date for the summary (YYYY-MM-DD format)')
    parser.add_argument('--token', type=str,
                        help='GitHub personal access token')
    return parser.parse_args()

def get_date_since(since_str=None):
    """Get the date since which to collect issues/PRs."""
    if since_str:
        try:
            return datetime.strptime(since_str, '%Y-%m-%d').date()
        except ValueError:
            print(f"Error: Invalid date format '{since_str}'. Use YYYY-MM-DD format.")
            sys.exit(1)
    else:
        # Default to 7 days ago
        return (datetime.now() - timedelta(days=7)).date()

def categorize_issue(issue):
    """Categorize issues based on labels and content."""
    labels = [label.name.lower() for label in issue.labels]
    
    # Check for specific categories based on labels
    if 'bug' in labels:
        return 'Bug Reports'
    elif 'enhancement' in labels or 'feature' in labels:
        return 'Feature Requests'
    elif 'good-first-issue' in labels:
        return 'Good First Issues'
    elif 'help-wanted' in labels:
        return 'Help Wanted'
    elif 'documentation' in labels:
        return 'Documentation'
    elif 'refactor' in [label.lower() for label in labels]:
        return 'Refactoring'
    elif any(comp in labels for comp in ['apiserver', 'ray-operator', 'dashboard']):
        return 'Component Specific'
    else:
        return 'General Issues'

def categorize_pr(pr):
    """Categorize PRs based on title and labels."""
    title = pr.title.lower()
    labels = [label.name.lower() for label in pr.labels]
    
    # Check for specific categories
    if '[bug]' in title or 'fix' in title:
        return 'Bug Fixes'
    elif '[feature]' in title or 'feat' in title:
        return 'New Features'
    elif '[refactor]' in title or 'refactor' in title:
        return 'Refactoring'
    elif '[chore]' in title or 'chore' in title:
        return 'Maintenance'
    elif 'test' in title:
        return 'Testing'
    elif 'doc' in title or 'documentation' in labels:
        return 'Documentation'
    elif 'bump' in title or 'dependencies' in labels:
        return 'Dependencies'
    else:
        return 'General Changes'

def generate_summary(github_token, since_date):
    """Generate the weekly summary."""
    try:
        g = Github(github_token)
        repo = g.get_repo("ray-project/kuberay")
    except Exception as e:
        print(f"Error connecting to GitHub: {e}")
        sys.exit(1)
    
    since_str = since_date.strftime('%Y-%m-%d')
    
    print(f"# KubeRay Weekly Summary - Week of {since_str}")
    print(f"*Generated on {datetime.now().strftime('%Y-%m-%d %H:%M:%S')} UTC*\n")
    
    # Search for issues and PRs created since the specified date
    try:
        issues_query = f"repo:ray-project/kuberay is:issue created:>={since_str}"
        prs_query = f"repo:ray-project/kuberay is:pr created:>={since_str}"
        
        issues = list(g.search_issues(issues_query))
        prs = list(g.search_issues(prs_query))
        
        print(f"## Summary Statistics")
        print(f"- **New Issues**: {len(issues)}")
        print(f"- **New Pull Requests**: {len(prs)}")
        print(f"- **Period**: {since_str} to {datetime.now().strftime('%Y-%m-%d')}")
        print()
        
        # Categorize issues
        if issues:
            print("## New Issues")
            issue_categories = defaultdict(list)
            for issue in issues:
                category = categorize_issue(issue)
                issue_categories[category].append(issue)
            
            for category, category_issues in sorted(issue_categories.items()):
                print(f"\n### {category} ({len(category_issues)})")
                for issue in category_issues:
                    state_icon = "🟢" if issue.state == "open" else "🔴"
                    labels_str = ", ".join([f"`{label.name}`" for label in issue.labels[:3]])
                    if len(issue.labels) > 3:
                        labels_str += "..."
                    print(f"- {state_icon} [#{issue.number}]({issue.html_url}) {issue.title}")
                    if labels_str:
                        print(f"  - Labels: {labels_str}")
                    print(f"  - Author: @{issue.user.login}")
        else:
            print("## New Issues\nNo new issues found in the specified time period.\n")
        
        # Categorize PRs
        if prs:
            print("\n## New Pull Requests")
            pr_categories = defaultdict(list)
            for pr in prs:
                category = categorize_pr(pr)
                pr_categories[category].append(pr)
            
            for category, category_prs in sorted(pr_categories.items()):
                print(f"\n### {category} ({len(category_prs)})")
                for pr in category_prs:
                    if pr.state == "open":
                        state_icon = "🟢"
                    elif getattr(pr, 'merged', False):
                        state_icon = "🟣"
                    else:
                        state_icon = "🔴"
                    
                    labels_str = ", ".join([f"`{label.name}`" for label in pr.labels[:3]])
                    if len(pr.labels) > 3:
                        labels_str += "..."
                    
                    print(f"- {state_icon} [#{pr.number}]({pr.html_url}) {pr.title}")
                    if labels_str:
                        print(f"  - Labels: {labels_str}")
                    print(f"  - Author: @{pr.user.login}")
        else:
            print("\n## New Pull Requests\nNo new pull requests found in the specified time period.\n")
        
        # Top contributors
        all_items = issues + prs
        if all_items:
            contributor_count = defaultdict(int)
            for item in all_items:
                contributor_count[item.user.login] += 1
            
            print("\n## Top Contributors This Week")
            sorted_contributors = sorted(contributor_count.items(), key=lambda x: x[1], reverse=True)
            for i, (username, count) in enumerate(sorted_contributors[:10], 1):
                activity_type = "contributions" if count > 1 else "contribution"
                print(f"{i}. @{username} - {count} {activity_type}")
        
        print(f"\n---")
        print(f"*This summary was generated using the weekly-summary.py script.*")
        print(f"*For the latest updates, visit the [KubeRay repository](https://github.com/ray-project/kuberay).*")
        
    except Exception as e:
        print(f"Error searching for issues/PRs: {e}")
        sys.exit(1)

def main():
    args = parse_args()
    
    # Get GitHub token
    github_token = args.token or os.getenv('GITHUB_TOKEN')
    if not github_token:
        print("Error: GitHub token is required. Use --token or set GITHUB_TOKEN environment variable.")
        sys.exit(1)
    
    # Get the date since which to collect data
    since_date = get_date_since(args.since)
    
    # Generate the summary
    generate_summary(github_token, since_date)

if __name__ == "__main__":
    main()