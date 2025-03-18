#!/usr/bin/env python3
import os
import re
import requests
import argparse
import json
import logging
import semver
from pathlib import Path
from datetime import datetime, timedelta

# Cache to store API results
cache = {}
cache_file = Path(os.path.expanduser("~/.github_action_sha_cache.json"))

# Set up logging
logger = logging.getLogger("github-action-sha-pinning")
console_handler = logging.StreamHandler()
formatter = logging.Formatter('%(levelname)s: %(message)s')
console_handler.setFormatter(formatter)
logger.addHandler(console_handler)

def load_cache():
    """Load the cache from disk if it exists."""
    global cache
    if not cache_file.exists():
        return

    try:
        with open(cache_file, 'r') as f:
            cache_data = json.load(f)
            # Check if cache is still valid (less than 24 hours old)
            if "timestamp" not in cache_data:
                return

            cache_time = datetime.fromisoformat(cache_data["timestamp"])
            if datetime.now() - cache_time >= timedelta(hours=24):
                return

            cache = cache_data.get("entries", {})
            logger.info(f"Loaded {len(cache)} entries from cache")
    except Exception as e:
        logger.error(f"Error loading cache: {e}")

def save_cache():
    """Save the cache to disk."""
    try:
        with open(cache_file, 'w', newline='') as f:
            json.dump({
                "timestamp": datetime.now().isoformat(),
                "entries": cache
            }, f)
        logger.info(f"Saved {len(cache)} entries to cache")
    except Exception as e:
        logger.error(f"Error saving cache: {e}")

def get_auth_headers():
    """Get authentication headers for GitHub API requests."""
    headers = {}
    if "GITHUB_TOKEN" in os.environ:
        headers["Authorization"] = f"token {os.environ['GITHUB_TOKEN']}"
        logger.debug("Using GITHUB_TOKEN for authentication")
    else:
        logger.debug("No GITHUB_TOKEN found, proceeding without authentication")
    return headers

def fetch_json(url, headers=None):
    """
    Fetch JSON data from a URL with proper error handling and logging.

    Args:
        url (str): URL to fetch data from
        headers (dict, optional): HTTP headers to include

    Returns:
        dict/list: JSON data if successful, None otherwise
    """
    if headers is None:
        headers = get_auth_headers()

    logger.info(f"Fetching: {url}")
    try:
        response = requests.get(url, headers=headers)
        logger.debug(f"Response status: {response.status_code}")

        if logger.isEnabledFor(logging.DEBUG):
            logger.debug(f"Response headers: {json.dumps(dict(response.headers), indent=2)}")

        if response.status_code != 200:
            logger.error(f"Error fetching {url}: {response.status_code}")
            if logger.isEnabledFor(logging.DEBUG):
                logger.debug(f"Response body: {response.text}")
            return None

        return response.json()
    except requests.exceptions.RequestException as e:
        logger.error(f"Request error for {url}: {e}")
        return None
    except json.JSONDecodeError as e:
        logger.error(f"Error parsing JSON from {url}: {e}")
        return None

def get_all_releases(repo):
    """Fetch all releases for a repo to find the latest versions."""
    releases_url = f"https://api.github.com/repos/{repo}/releases"
    return fetch_json(releases_url) or []

def parse_semver(version_tag):
    """
    Parse a version tag into a semver object with error handling.

    Args:
        version_tag (str): Version tag to parse (e.g., "v1.2.3" or "1.2.3")

    Returns:
        tuple: (success (bool), parsed version or None)
    """
    # Strip 'v' prefix if present for semver comparison
    version = version_tag
    if version.startswith('v'):
        version = version[1:]

    try:
        return True, semver.VersionInfo.parse(version)
    except ValueError:
        logger.debug(f"Could not parse {version_tag} as semver")
        return False, None

def get_latest_version(repo, current_tag):
    """Get the latest version for a repo based on the current tag series."""
    try:
        # Parse the current version
        success, parsed_current = parse_semver(current_tag)
        if not success:
            return current_tag

        major_version = parsed_current.major

        # Get all releases
        releases = get_all_releases(repo)
        if not releases:
            return current_tag

        latest_version = current_tag
        latest_parsed = None

        # Find the latest version in the same major series
        for release in releases:
            tag_name = release["tag_name"]
            success, parsed = parse_semver(tag_name)
            if not success or parsed.major != major_version:
                continue

            if latest_parsed is None or parsed > latest_parsed:
                latest_parsed = parsed
                latest_version = tag_name

        return latest_version
    except Exception as e:
        logger.error(f"Error finding latest version: {e}")
        return current_tag

def get_tag_sha(repo, tag):
    """
    Get the SHA for a specific tag.

    Args:
        repo (str): Repository name (owner/repo)
        tag (str): Tag name

    Returns:
        str or None: SHA if found, None otherwise
    """
    ref_url = f"https://api.github.com/repos/{repo}/git/refs/tags/{tag}"
    ref_data = fetch_json(ref_url)

    if not ref_data or "object" not in ref_data:
        return None

    sha = ref_data["object"]["sha"]
    logger.info(f"Found SHA: {sha}")
    return sha

def get_exact_tag_match(repo, tag):
    """
    Try to find an exact tag match in the repository.

    Args:
        repo (str): Repository name (owner/repo)
        tag (str): Tag to match

    Returns:
        str: Matched tag name or original tag if no match found
    """
    tag_url = f"https://api.github.com/repos/{repo}/tags"
    tags_data = fetch_json(tag_url)

    if not tags_data:
        return tag

    if logger.isEnabledFor(logging.DEBUG):
        logger.debug(f"Tags data: {json.dumps(tags_data[:2], indent=2)}... (truncated)")

    for tag_info in tags_data:
        if tag_info["name"] == tag:
            logger.info(f"Found exact tag match: {tag}")
            return tag

    return tag

def get_latest_release_tag(repo, tag):
    """
    Get the latest release tag for short version tags like v1.

    Args:
        repo (str): Repository name (owner/repo)
        tag (str): Tag prefix (e.g., "v1")

    Returns:
        str: Full release tag or original tag if not found
    """
    # Only process short version tags
    if not tag.startswith('v') or len(tag) > 3:
        return tag

    release_url = f"https://api.github.com/repos/{repo}/releases/latest"
    release_data = fetch_json(release_url)

    if not release_data or "tag_name" not in release_data:
        return tag

    latest_tag = release_data["tag_name"]
    if not latest_tag.startswith(tag):
        return tag

    logger.info(f"Using latest release tag: {latest_tag}")
    return latest_tag

def get_cached_details(repo, tag, check_for_updates):
    """Get cached details for a repository and tag if available and valid."""
    cache_key = f"{repo}@{tag}"
    if not check_for_updates and cache_key in cache:
        logger.info(f"Using cached data for {cache_key}")
        return cache[cache_key]
    return None

def update_cache(repo, tag, sha, full_version):
    """Update cache with new repository tag details."""
    cache_key = f"{repo}@{tag}"
    cache[cache_key] = {"sha": sha, "full_version": full_version}
    logger.info(f"Cached {cache_key} -> SHA: {sha}, Version: {full_version}")

def fetch_tag_details(repo, tag, check_for_updates=False):
    """Fetch both the SHA and full semver version for a GitHub action tag with caching."""
    # Check cache first
    details = get_cached_details(repo, tag, check_for_updates)
    if details:
        return details["sha"], details["full_version"]

    # Check for newer versions if requested
    if not check_for_updates:
        latest_tag = tag
    else:
        latest_tag = get_latest_version(repo, tag)
        if latest_tag != tag:
            logger.info(f"Found newer version for {repo}: {latest_tag} (current: {tag})")
            # Check cache for newer version
            details = get_cached_details(repo, latest_tag, check_for_updates)
            if details:
                return details["sha"], details["full_version"]

    # Get the SHA for this tag
    sha = get_tag_sha(repo, latest_tag)
    if not sha:
        return None, None

    # Get the full version info
    full_version = get_exact_tag_match(repo, latest_tag)
    if full_version == latest_tag:
        full_version = get_latest_release_tag(repo, latest_tag)

    # Update cache with new details
    update_cache(repo, latest_tag, sha, full_version)
    return sha, full_version

def extract_comment_version(line):
    """Extract the version from a comment in a pinned line."""
    if comment_match := re.search(r'#\s*(v\d+(?:\.\d+)*)', line):
        return comment_match[1]
    return None

def is_pinned_action(line):
    """Check if a line contains a SHA-pinned action."""
    # Pattern to match SHA pinned actions: owner/repo@abcd1234 # v1.2.3
    pattern = r'([\w-]+/[\w-]+)@([0-9a-f]{40})\s*#\s*(v\d+(?:\.\d+)*)'
    return bool(re.search(pattern, line))

def process_pinned_action(line, check_for_updates):
    """Process a line containing a pinned GitHub Action."""
    if not check_for_updates:
        return line, False

    pattern = r'([\w-]+/[\w-]+)@([0-9a-f]{40})\s*#\s*(v\d+(?:\.\d+)*)'
    if not (match := re.search(pattern, line)):
        return line, False

    repo = match[1]
    current_version = match[3]
    logger.info(f"Found pinned action: {repo}@{current_version}")

    # Get the latest version and its SHA
    sha, full_version = fetch_tag_details(repo, current_version, check_for_updates=True)
    if not sha or full_version == current_version:
        return line, False

    new_line = re.sub(pattern, f"{repo}@{sha} # {full_version}", line)
    logger.info(f"Upgraded: {repo}@{current_version} -> {full_version}")
    return new_line, True

def process_unpinned_action(line, check_for_updates):
    """Process a line containing an unpinned GitHub Action."""
    pattern = r'([\w-]+/[\w-]+)@(v\d+(?:\.\d+)*)'
    if not (match := re.search(pattern, line)):
        return line, False

    repo = match[1]
    tag = match[2]
    key = f"{repo}@{tag}"
    logger.info(f"Found unpinned action: {key}")

    sha, full_version = fetch_tag_details(repo, tag, check_for_updates=check_for_updates)
    if not sha:
        return line, False

    new_line = line.replace(key, f"{repo}@{sha} # {full_version}")
    logger.info(f"Pinned: {key} -> {repo}@{sha} # {full_version}")
    return new_line, True

def process_line(line, check_for_updates):
    """Process a single line from a workflow file."""
    return (process_pinned_action if is_pinned_action(line) else process_unpinned_action)(line, check_for_updates)

def process_workflow_file(file_path, check_for_updates=False):
    """Process a workflow file to update actions with SHA pinning."""
    logger.info(f"Processing: {file_path}")

    try:
        with open(file_path, 'r', newline='') as file:
            content = file.read()
    except Exception as e:
        logger.error(f"Error reading file {file_path}: {e}")
        return

    lines = content.splitlines()
    updated_lines = []
    changes_made = False

    # Process each line
    for line in lines:
        updated_line, line_changed = process_line(line, check_for_updates)
        updated_lines.append(updated_line)
        changes_made = changes_made or line_changed

    # Write updated content back to file if changes were made
    if not changes_made:
        logger.info(f"No changes needed for {file_path}")
        return

    try:
        new_content = '\n'.join(updated_lines)
        with open(file_path, 'w', newline='') as file:
            file.write(new_content)
        logger.info(f"Updated {file_path}")
    except Exception as e:
        logger.error(f"Error writing file {file_path}: {e}")

def main():
    parser = argparse.ArgumentParser(description='Update GitHub Action references to use SHA pinning with full semver comments')
    parser.add_argument('directory', help='Directory containing workflow files')
    parser.add_argument('--dry-run', action='store_true', help='Show changes without modifying files')
    parser.add_argument('--clear-cache', action='store_true', help='Clear the cache before running')
    parser.add_argument('--upgrade', action='store_true', help='Check for newer versions and upgrade pins')
    parser.add_argument('-v', '--verbose', action='count', default=0, help='Increase verbosity (can be used multiple times)')
    args = parser.parse_args()

    # Set up logging level based on verbosity
    log_level = logging.DEBUG if args.verbose > 0 else logging.INFO
    logger.setLevel(log_level)

    if args.verbose >= 2:
        # Enable HTTP request logging
        requests_log = logging.getLogger("requests.packages.urllib3")
        requests_log.setLevel(logging.DEBUG)
        requests_log.propagate = True

    logger.info(f"Verbosity level: {args.verbose}")
    logger.info(f"Upgrade mode: {args.upgrade}")

    # Handle cache
    if args.clear_cache and cache_file.exists():
        os.remove(cache_file)
        logger.info("Cache cleared")
    else:
        load_cache()

    # Find all workflow files
    try:
        workflow_dir = Path(args.directory)
        if not workflow_dir.exists():
            logger.error(f"Directory not found: {args.directory}")
            return

        # Look for workflow files (*.yml, *.yaml) in the provided directory
        workflow_files = []
        for extension in ['.yml', '.yaml']:
            workflow_files.extend(workflow_dir.glob(f"**/*{extension}"))

        if not workflow_files:
            logger.warning(f"No workflow files found in {args.directory}")
            return

        logger.info(f"Found {len(workflow_files)} workflow files")

        # Process each workflow file
        for file_path in workflow_files:
            if args.dry_run:
                logger.info(f"Would process: {file_path} (dry run)")
                continue
            process_workflow_file(file_path, check_for_updates=args.upgrade)

        # Save cache after processing
        save_cache()

    except Exception as e:
        logger.error(f"Unexpected error: {e}")
        import traceback
        logger.debug(traceback.format_exc())

if __name__ == "__main__":
    main()