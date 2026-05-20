"""AI abuse-report triage for Phaze Nexus.

Pulls pending reports from /api/v1/admin/reports, classifies each via the
Google Gemini API, and:

  * Auto-resolves reports marked "false_positive" with confidence ≥ 0.85.
  * Opens a single batched GitHub issue for everything needing human review.
  * NEVER auto-bans. Bans are always a human decision.

Required env:
    GOOGLE_API_KEY      - Google AI Studio key (free tier: aistudio.google.com)
    NEXUS_ADMIN_TOKEN   - bearer for an admin user's session_token
    NEXUS_BASE_URL      - e.g. https://phazechat.world
    GH_TOKEN            - GITHUB_TOKEN (provided by Actions)
    GH_REPO             - owner/name (provided by Actions)
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from dataclasses import dataclass

import google.generativeai as genai
import httpx

# -----------------------------------------------------------------------
# Configuration
# -----------------------------------------------------------------------

MODEL = "gemini-2.0-flash"
AUTO_RESOLVE_CATEGORIES = {"false_positive"}
AUTO_RESOLVE_MIN_CONFIDENCE = 0.85
MAX_REPORTS_PER_RUN = 50
HTTP_TIMEOUT = httpx.Timeout(15.0, connect=5.0)


# -----------------------------------------------------------------------
# Types
# -----------------------------------------------------------------------

@dataclass
class Report:
    id: int
    reporter: str
    subject: str
    reason: str
    body: str
    created_at: str


@dataclass
class Classification:
    category: str       # false_positive | spam | harassment | illegal | other
    confidence: float   # 0.0–1.0
    reasoning: str


# -----------------------------------------------------------------------
# Nexus admin API
# -----------------------------------------------------------------------

def nexus_auth_headers() -> dict[str, str]:
    return {"Authorization": f"Bearer {require_env('NEXUS_ADMIN_TOKEN')}"}


def fetch_pending_reports(base_url: str) -> list[Report]:
    url = f"{base_url.rstrip('/')}/api/v1/admin/reports?status=pending"
    with httpx.Client(timeout=HTTP_TIMEOUT) as client:
        resp = client.get(url, headers=nexus_auth_headers())
    resp.raise_for_status()
    return [
        Report(
            id=int(r["id"]),
            reporter=r.get("reporter", ""),
            subject=r.get("subject", ""),
            reason=r.get("reason", ""),
            body=r.get("body", ""),
            created_at=r.get("created_at", ""),
        )
        for r in (resp.json() or [])
    ]


def resolve_report(base_url: str, report_id: int) -> None:
    url = f"{base_url.rstrip('/')}/api/v1/admin/reports/{report_id}/resolve"
    with httpx.Client(timeout=HTTP_TIMEOUT) as client:
        resp = client.post(url, headers=nexus_auth_headers())
    resp.raise_for_status()


# -----------------------------------------------------------------------
# Classification (Gemini)
# -----------------------------------------------------------------------

SYSTEM_PROMPT = (
    "You are a content-moderation triage assistant for a sovereign chat "
    "platform (Phaze). For each abuse report decide which category fits "
    "and how confident you are.\n\n"
    "Categories:\n"
    "  - false_positive: clearly not actionable (accidental click, joke, "
    "    benign content the reporter misunderstood). Only use when confident.\n"
    "  - spam: commercial promotion, mass identical messages, scam links.\n"
    "  - harassment: targeted hostility, threats, doxxing, hate speech.\n"
    "  - illegal: CSAM, terrorism, doxxing of minors, NCII. Always escalate.\n"
    "  - other: anything that doesn't cleanly fit. Always escalate.\n\n"
    "Respond ONLY with JSON: "
    '{"category":"...","confidence":0.0-1.0,"reasoning":"one sentence"}'
)


def classify(model: genai.GenerativeModel, report: Report) -> Classification:
    prompt = (
        f"{SYSTEM_PROMPT}\n\n"
        f"Reporter: {report.reporter}\n"
        f"Subject (reported user): {report.subject}\n"
        f"Reason tag: {report.reason}\n"
        f"Report body: {report.body or '(empty)'}\n"
        f"Filed at: {report.created_at}"
    )
    response = model.generate_content(
        prompt,
        generation_config=genai.GenerationConfig(max_output_tokens=300),
    )
    text = response.text.strip()
    if text.startswith("```"):
        text = text.strip("`")
        if text.startswith("json"):
            text = text[4:].lstrip()
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return Classification("other", 0.0, f"could not parse model output: {text!r}")
    return Classification(
        category=str(data.get("category", "other")),
        confidence=float(data.get("confidence", 0.0)),
        reasoning=str(data.get("reasoning", "")),
    )


# -----------------------------------------------------------------------
# Escalation (GitHub issue)
# -----------------------------------------------------------------------

def open_escalation_issue(base_url: str, escalations: list[tuple[Report, Classification]]) -> None:
    if not escalations:
        return
    gh_token = require_env("GH_TOKEN")
    repo = require_env("GH_REPO")

    body_lines = [
        f"AI triage flagged **{len(escalations)} report(s)** that need human review.",
        "",
        "Bans are *never* issued automatically — review and act below.",
        "",
        "| # | Category | Conf | Reporter → Subject | Reason | AI says |",
        "| - | -------- | ---- | ------------------ | ------ | ------- |",
    ]
    for r, c in escalations:
        body_lines.append(
            f"| `{r.id}` | `{c.category}` | {c.confidence:.2f} | "
            f"`{r.reporter}` → `{r.subject}` | {r.reason} | "
            f"{c.reasoning.replace('|', '/')} |"
        )

    body_lines += [
        "",
        "### Operator quickstart",
        "```sh",
        f"export NEXUS_BASE='{base_url}'",
        "export NEXUS_TOK='<your admin session token>'",
        "```",
        "",
        "Resolve (no action):",
        "```sh",
        'curl -X POST "$NEXUS_BASE/api/v1/admin/reports/<id>/resolve" \\',
        '  -H "Authorization: Bearer $NEXUS_TOK"',
        "```",
        "",
        "Ban subject:",
        "```sh",
        'curl -X POST "$NEXUS_BASE/api/v1/admin/users/<username>/ban" \\',
        '  -H "Authorization: Bearer $NEXUS_TOK" \\',
        "  -H 'Content-Type: application/json' \\",
        "  -d '{\"reason\":\"<reason shown to user>\"}'",
        "```",
    ]

    categories = sorted({c.category for _, c in escalations})
    title = f"AI triage: {len(escalations)} report(s) need review ({', '.join(categories)})"
    issue_body = "\n".join(body_lines)

    try:
        subprocess.run(
            ["gh", "issue", "create", "--title", title, "--body", issue_body,
             "--label", "ai-triage", "--label", "moderation"],
            check=True,
            env={**os.environ, "GH_TOKEN": gh_token},
        )
        return
    except (FileNotFoundError, subprocess.CalledProcessError):
        pass

    with httpx.Client(timeout=HTTP_TIMEOUT) as client:
        resp = client.post(
            f"https://api.github.com/repos/{repo}/issues",
            content=json.dumps({"title": title, "body": issue_body, "labels": ["ai-triage", "moderation"]}),
            headers={
                "Authorization": f"Bearer {gh_token}",
                "Accept": "application/vnd.github+json",
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
    resp.raise_for_status()


# -----------------------------------------------------------------------
# Glue
# -----------------------------------------------------------------------

def require_env(name: str) -> str:
    v = os.environ.get(name, "").strip()
    if not v:
        sys.stderr.write(f"missing env: {name}\n")
        sys.exit(2)
    return v


def main() -> int:
    base_url = require_env("NEXUS_BASE_URL")
    google_key = require_env("GOOGLE_API_KEY")

    try:
        reports = fetch_pending_reports(base_url)
    except httpx.HTTPError as e:
        sys.stderr.write(f"fetch reports: {e}\n")
        return 1

    if not reports:
        print("no pending reports; nothing to triage")
        return 0

    if len(reports) > MAX_REPORTS_PER_RUN:
        print(f"capping {len(reports)} → {MAX_REPORTS_PER_RUN} this run")
        reports = reports[:MAX_REPORTS_PER_RUN]

    genai.configure(api_key=google_key)
    model = genai.GenerativeModel(MODEL)

    auto_resolved: list[tuple[Report, Classification]] = []
    escalations: list[tuple[Report, Classification]] = []

    for r in reports:
        try:
            c = classify(model, r)
        except Exception as e:
            sys.stderr.write(f"classify {r.id}: {e}\n")
            escalations.append((r, Classification("other", 0.0, f"classifier error: {e}")))
            continue

        if c.category in AUTO_RESOLVE_CATEGORIES and c.confidence >= AUTO_RESOLVE_MIN_CONFIDENCE:
            try:
                resolve_report(base_url, r.id)
                auto_resolved.append((r, c))
                print(f"auto-resolved {r.id} ({c.category} @ {c.confidence:.2f}): {c.reasoning}")
            except httpx.HTTPError as e:
                sys.stderr.write(f"resolve {r.id}: {e}\n")
                escalations.append((r, c))
        else:
            escalations.append((r, c))
            print(f"escalating {r.id} ({c.category} @ {c.confidence:.2f}): {c.reasoning}")

    if escalations:
        try:
            open_escalation_issue(base_url, escalations)
        except Exception as e:
            sys.stderr.write(f"escalation issue: {e}\n")

    print(f"summary: auto-resolved={len(auto_resolved)} escalated={len(escalations)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
