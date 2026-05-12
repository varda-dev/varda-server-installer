#!/usr/bin/env python3

from __future__ import annotations

import argparse
import http.client
import json
import mimetypes
import os
import re
import sys
import time
from pathlib import Path
from typing import Any
from urllib.parse import quote, urlencode, urlsplit


REPO_ROOT = Path(__file__).resolve().parent
DEFAULT_ENV_PATH = REPO_ROOT / ".env"
UPLOAD_DIR = REPO_ROOT / "tmp" / "release"
GITHUB_REPOSITORY = "varda-dev/varda-server-installer"
GITHUB_PAT_RELEASES = "GITHUB_PAT_RELEASES"
GITHUB_API_VERSION = "2022-11-28"
DEFAULT_GITHUB_API_BASE = "https://api.github.com"
DEFAULT_GITHUB_API_MAX_ATTEMPTS = 3
DEFAULT_GITHUB_RETRY_BASE_DELAY = 5
RETRYABLE_HTTP_STATUSES = {429, 500, 502, 503, 504}


class GitHubApiError(RuntimeError):
  def __init__(self, message: str, *, http_status: int | None = None):
    super().__init__(message)
    self.http_status = http_status


def fail(message: str) -> None:
  print(message, file=sys.stderr, flush=True)
  raise SystemExit(1)


def load_dotenv(path: Path = DEFAULT_ENV_PATH) -> dict[str, str]:
  if not path.is_file():
    raise FileNotFoundError(f"Missing .env file: {path}")

  values: dict[str, str] = {}
  for line in path.read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line:
      continue
    if line.startswith("export "):
      line = line[len("export ") :].strip()
    key, value = line.split("=", 1)
    key = key.strip()
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
      value = value[1:-1]
    if key:
      values[key] = value

  return values


def env_required(values: dict[str, str], name: str) -> str:
  value = values.get(name, "").strip()
  if not value:
    raise ValueError(f"Missing required .env value: {name}")
  return value


def get_github_releases_pat() -> str:
  raw_value = os.environ.get(GITHUB_PAT_RELEASES, "").strip()
  if raw_value:
    return raw_value
  return env_required(load_dotenv(), GITHUB_PAT_RELEASES)


def slugify_version(value: str) -> str:
  value = value.strip()
  if not value:
    fail("Version cannot be empty")
  if not re.fullmatch(r"[A-Za-z0-9._-]+", value):
    fail("Version may only contain letters, numbers, dots, underscores, and hyphens")
  return value


def response_body_to_text(body: Any) -> str:
  if body is None:
    return ""
  if isinstance(body, (dict, list)):
    return json.dumps(body, indent=2, ensure_ascii=False)
  if isinstance(body, bytes):
    return body.decode("utf-8", errors="replace")
  return str(body)


def request_url_parts(url: str, *, label: str = "URL") -> tuple[str, str, str]:
  parsed = urlsplit(url)
  if parsed.scheme not in {"http", "https"}:
    fail(f"Unsupported {label} scheme: {parsed.scheme}")
  if not parsed.netloc:
    fail(f"{label} is missing a host: {url}")
  path = parsed.path or "/"
  if parsed.query:
    path = f"{path}?{parsed.query}"
  return parsed.scheme, parsed.netloc, path


def parse_response_body(response: http.client.HTTPResponse, raw: bytes) -> Any:
  if not raw:
    return None
  content_type = (response.getheader("Content-Type") or "").lower()
  if "json" in content_type:
    try:
      return json.loads(raw.decode("utf-8"))
    except json.JSONDecodeError as err:
      raise GitHubApiError(f"HTTP response returned invalid JSON for status {response.status}: {err}") from err
  if "text/" in content_type or "charset=" in content_type:
    return raw.decode("utf-8", errors="replace")
  try:
    return raw.decode("utf-8")
  except UnicodeDecodeError:
    return raw


def http_request(
  method: str,
  url: str,
  *,
  headers: dict[str, str] | None = None,
  json_body: Any | None = None,
  raw_body: bytes | None = None,
  timeout: int = 120,
  retryable_statuses: set[int] | None = None,
  max_attempts: int = 1,
  retry_base_delay: int = 5,
  retry_label: str = "HTTP request",
) -> tuple[int, Any, dict[str, str]]:
  if json_body is not None and raw_body is not None:
    fail("http_request accepts either json_body or raw_body, not both")

  retryable_statuses = retryable_statuses or set()
  status = 0
  body: Any = None
  response_headers: dict[str, str] = {}
  scheme, host, path = request_url_parts(url)
  request_headers = dict(headers or {})

  if json_body is not None:
    raw_body = json.dumps(json_body, ensure_ascii=False).encode("utf-8")
    request_headers.setdefault("Content-Type", "application/json")

  if raw_body is not None:
    request_headers["Content-Length"] = str(len(raw_body))

  connection_class = http.client.HTTPSConnection if scheme == "https" else http.client.HTTPConnection

  for attempt in range(1, max_attempts + 1):
    connection = connection_class(host, timeout=timeout)
    try:
      connection.request(method, path, body=raw_body, headers=request_headers)
      response = connection.getresponse()
      raw = response.read()
      status = response.status
      body = parse_response_body(response, raw)
      response_headers = {key.lower(): value for key, value in response.headers.items()}
    except (OSError, http.client.HTTPException) as err:
      raise GitHubApiError(f"{retry_label} failed: {err}") from err
    finally:
      connection.close()

    if status not in retryable_statuses or attempt >= max_attempts:
      return status, body, response_headers

    delay = retry_base_delay * attempt
    retry_after = response_headers.get("retry-after")
    if retry_after:
      try:
        delay = max(delay, int(retry_after))
      except ValueError:
        pass
    print(f"{retry_label} returned HTTP {status}; retrying in {delay}s.", file=sys.stderr)
    time.sleep(delay)

  return status, body, response_headers


def github_api_request(
  method: str,
  url: str,
  token: str,
  *,
  json_body: Any | None = None,
  headers: dict[str, str] | None = None,
  raw_body: bytes | None = None,
) -> tuple[int, Any]:
  try:
    request_headers = {
      "Accept": "application/vnd.github+json",
      "Authorization": f"Bearer {token}",
      "X-GitHub-Api-Version": GITHUB_API_VERSION,
      "User-Agent": "varda-release-uploader/1.0",
    }
    if headers:
      request_headers.update(headers)

    status, body, _response_headers = http_request(
      method,
      url,
      headers=request_headers,
      json_body=json_body,
      raw_body=raw_body,
      timeout=120,
      retryable_statuses=RETRYABLE_HTTP_STATUSES,
      max_attempts=DEFAULT_GITHUB_API_MAX_ATTEMPTS,
      retry_base_delay=DEFAULT_GITHUB_RETRY_BASE_DELAY,
      retry_label="GitHub API request",
    )
    return status, body
  except GitHubApiError as err:
    raise GitHubApiError(str(err), http_status=err.http_status) from err


def raise_github_api_error(message: str, status: int, body: Any) -> None:
  details = response_body_to_text(body).strip()
  if details:
    raise GitHubApiError(f"{message}: HTTP {status}\n{details}", http_status=status)
  raise GitHubApiError(f"{message}: HTTP {status}", http_status=status)


def split_repository(repository: str) -> tuple[str, str]:
  value = repository.strip()
  if not value or "/" not in value:
    fail(f"Invalid GitHub repository value: {repository!r}")
  owner, name = value.split("/", 1)
  owner = owner.strip()
  name = name.strip()
  if not owner or not name:
    fail(f"Invalid GitHub repository value: {repository!r}")
  return owner, name


def repository_path(repository: str) -> str:
  owner, name = split_repository(repository)
  return f"/repos/{quote(owner, safe='')}/{quote(name, safe='')}"


def release_tag_for_version(version: str) -> str:
  return f"v{version}"


def release_name_for_version(version: str) -> str:
  return f"Varda Server Installer {version}"


def resolve_server_installer_assets(version: str) -> list[Path]:
  pattern = f"varda-server-installer-{version}-*"
  matches = [
    path
    for path in sorted(UPLOAD_DIR.glob(pattern), key=lambda candidate: candidate.name)
    if path.is_file()
  ]
  if not matches:
    fail(
      "No release archives were found in tmp/release. "
      f"Expected files matching {pattern}.zip or {pattern}.tar.gz."
    )

  archives = [
    path
    for path in matches
    if path.name.endswith(".zip") or path.name.endswith(".tar.gz")
  ]
  if not archives:
    fail("No release archives found. Upload archives only, not raw binaries.")

  disallowed = [
    path
    for path in matches
    if path not in archives
  ]
  if disallowed:
    names = ", ".join(path.name for path in disallowed)
    fail(f"Found non-archive release files in tmp/release: {names}")

  return archives


def read_checksums(path: Path) -> dict[str, str]:
  if not path.is_file():
    fail(f"Missing checksums file: {path}")

  checksums: dict[str, str] = {}
  for line in path.read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if not line:
      continue
    parts = line.split()
    if len(parts) != 2:
      fail(f"Invalid checksums line: {line!r}")
    sha256, name = parts
    if len(sha256) != 64 or any(ch not in "0123456789abcdefABCDEF" for ch in sha256):
      fail(f"Invalid sha256 in checksums.txt for {name!r}")
    checksums[name] = sha256
  return checksums


def validate_checksums(archives: list[Path], checksums_path: Path) -> None:
  checksums = read_checksums(checksums_path)
  archive_names = {path.name for path in archives}
  checksum_names = set(checksums)

  missing = sorted(archive_names - checksum_names)
  extra = sorted(checksum_names - archive_names)
  if missing:
    fail(f"checksums.txt missing entries for: {', '.join(missing)}")
  if extra:
    fail(f"checksums.txt has unexpected entries for: {', '.join(extra)}")


def get_release_by_tag(token: str, tag: str) -> dict[str, Any] | None:
  url = f"{DEFAULT_GITHUB_API_BASE}{repository_path(GITHUB_REPOSITORY)}/releases/tags/{quote(tag, safe='')}"
  status, body = github_api_request("GET", url, token)
  if status == 404:
    return None
  if status < 200 or status >= 300:
    raise_github_api_error(f"Failed to fetch GitHub release for tag {tag!r}", status, body)
  if not isinstance(body, dict):
    fail("GitHub release lookup returned an unexpected response.")
  return body


def create_release(
  token: str,
  *,
  tag: str,
  name: str,
  body: str,
  draft: bool,
  prerelease: bool,
) -> dict[str, Any]:
  url = f"{DEFAULT_GITHUB_API_BASE}{repository_path(GITHUB_REPOSITORY)}/releases"
  status, response = github_api_request(
    "POST",
    url,
    token,
    json_body={
      "tag_name": tag,
      "name": name,
      "body": body,
      "draft": draft,
      "prerelease": prerelease,
    },
  )
  if status < 200 or status >= 300:
    raise_github_api_error(f"Failed to create GitHub release for tag {tag!r}", status, response)
  if not isinstance(response, dict):
    fail("GitHub release creation returned an unexpected response.")
  return response


def update_release(
  token: str,
  release: dict[str, Any],
  *,
  name: str,
  body: str,
  draft: bool,
  prerelease: bool,
) -> dict[str, Any]:
  release_id = release.get("id")
  if not isinstance(release_id, int):
    fail("GitHub release response did not include a numeric id.")
  url = f"{DEFAULT_GITHUB_API_BASE}{repository_path(GITHUB_REPOSITORY)}/releases/{release_id}"
  status, response = github_api_request(
    "PATCH",
    url,
    token,
    json_body={
      "name": name,
      "body": body,
      "draft": draft,
      "prerelease": prerelease,
    },
  )
  if status < 200 or status >= 300:
    raise_github_api_error(f"Failed to update GitHub release {release_id!r}", status, response)
  if not isinstance(response, dict):
    fail("GitHub release update returned an unexpected response.")
  return response


def list_release_assets(token: str, release: dict[str, Any]) -> list[dict[str, Any]]:
  assets_url = release.get("assets_url")
  if not isinstance(assets_url, str) or not assets_url:
    fail("GitHub release response did not include an assets_url.")
  status, response = github_api_request("GET", assets_url, token)
  if status < 200 or status >= 300:
    raise_github_api_error("Failed to list GitHub release assets", status, response)
  if not isinstance(response, list):
    fail("GitHub release assets response returned an unexpected response.")
  assets: list[dict[str, Any]] = []
  for item in response:
    if isinstance(item, dict):
      assets.append(item)
  return assets


def delete_release_asset(token: str, asset: dict[str, Any]) -> None:
  asset_url = asset.get("url")
  if not isinstance(asset_url, str) or not asset_url:
    fail("GitHub release asset did not include a url.")
  status, response = github_api_request("DELETE", asset_url, token)
  if status == 404:
    return
  if status < 200 or status >= 300:
    raise_github_api_error("Failed to delete GitHub release asset", status, response)


def upload_release_asset(token: str, release: dict[str, Any], asset_path: Path) -> dict[str, Any]:
  upload_url = release.get("upload_url")
  if not isinstance(upload_url, str) or not upload_url:
    fail("GitHub release response did not include an upload_url.")
  base_upload_url = upload_url.split("{", 1)[0]
  asset_url = f"{base_upload_url}?{urlencode({'name': asset_path.name})}"
  content_type = mimetypes.guess_type(asset_path.name)[0] or "application/octet-stream"
  raw_body = asset_path.read_bytes()
  status, response = github_api_request(
    "POST",
    asset_url,
    token,
    headers={"Content-Type": content_type},
    raw_body=raw_body,
  )
  if status < 200 or status >= 300:
    raise_github_api_error(
      f"Failed to upload GitHub release asset {asset_path.name!r}",
      status,
      response,
    )
  if not isinstance(response, dict):
    fail(f"GitHub upload response for {asset_path.name!r} returned an unexpected response.")
  return response


def validate_release_assets(
  assets: list[Path],
  release: dict[str, Any] | None,
  *,
  replace_assets: bool,
  token: str,
) -> list[Path]:
  if release is None:
    return assets

  existing_assets = list_release_assets(token, release)
  existing_by_name = {asset_name: asset for asset in existing_assets if isinstance((asset_name := asset.get("name")), str) and asset_name}

  for asset_path in assets:
    existing_asset = existing_by_name.get(asset_path.name)
    if existing_asset is None:
      continue
    if not replace_assets:
      fail(
        f"GitHub release already has an asset named {asset_path.name!r}. "
        "Pass --replace-assets to delete it first."
      )
    delete_release_asset(token, existing_asset)

  return assets


def print_summary(
  *,
  repository: str,
  tag: str,
  name: str,
  draft: bool,
  prerelease: bool,
  asset_paths: list[Path],
  checksums_path: Path,
) -> None:
  print("GitHub release upload:")
  print(f"  repo:         {repository}")
  print(f"  tag:          {tag}")
  print(f"  name:         {name}")
  print(f"  draft:        {draft}")
  print(f"  prerelease:   {prerelease}")
  print("  assets:")
  for asset_path in asset_paths:
    print(f"    - {asset_path}")
  print(f"    - {checksums_path}")


def parse_args() -> argparse.Namespace:
  parser = argparse.ArgumentParser(description="Upload Varda server installer binaries to GitHub Releases.")
  parser.add_argument("-v", "--version", required=True, help="Version string, example: 1.0.0.")
  parser.add_argument("-c", "--changelog", required=True, help="Release body text.")
  parser.add_argument("--tag", help="Override the release tag.")
  parser.add_argument("--name", help="Override the release name.")
  parser.add_argument("--draft", action="store_true", help="Create or keep the release as a draft.")
  parser.add_argument("--prerelease", action="store_true", help="Force the release to be marked as a prerelease.")
  parser.add_argument("--replace-assets", action="store_true", help="Delete same-name release assets before reuploading them.")
  parser.add_argument("--dry-run", action="store_true", help="Print the planned GitHub release actions without making API calls.")
  return parser.parse_args()


def main() -> int:
  args = parse_args()

  try:
    version = slugify_version(args.version)
  except (OSError, RuntimeError, ValueError) as err:
    print(f"error: {err}", file=sys.stderr)
    return 1

  repository = GITHUB_REPOSITORY
  tag = args.tag or release_tag_for_version(version)
  name = args.name or release_name_for_version(version)
  prerelease = args.prerelease
  asset_paths = resolve_server_installer_assets(version)
  checksums_path = UPLOAD_DIR / "checksums.txt"
  validate_checksums(asset_paths, checksums_path)

  print_summary(
    repository=repository,
    tag=tag,
    name=name,
    draft=args.draft,
    prerelease=prerelease,
    asset_paths=asset_paths,
    checksums_path=checksums_path,
  )

  if args.dry_run:
    print("Dry run: no GitHub API calls were made.")
    return 0

  try:
    token = get_github_releases_pat()
    release = get_release_by_tag(token, tag)
    if release is None:
      release = create_release(
        token,
        tag=tag,
        name=name,
        body=args.changelog,
        draft=args.draft,
        prerelease=prerelease,
      )
    else:
      release = update_release(
        token,
        release,
        name=name,
        body=args.changelog,
        draft=args.draft,
        prerelease=prerelease,
      )

    validated_assets = validate_release_assets(
      [*asset_paths, checksums_path],
      release,
      replace_assets=args.replace_assets,
      token=token,
    )

    for asset_path in validated_assets:
      upload_release_asset(token, release, asset_path)

    html_url = release.get("html_url")
    if isinstance(html_url, str) and html_url:
      print(f"Release URL: {html_url}")
  except (OSError, RuntimeError, ValueError, GitHubApiError) as err:
    print(f"error: {err}", file=sys.stderr)
    return 1

  print("GitHub release upload successful.")
  return 0


if __name__ == "__main__":
  raise SystemExit(main())
