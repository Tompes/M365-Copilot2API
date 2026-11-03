#!/usr/bin/env python3
"""Export a personal Copilot session locally; never print credentials.

Install: python -m pip install playwright && python -m playwright install chromium
Run: python scripts/personal_login.py
Sign in yourself and send one short message in the opened browser.
"""
import argparse
import json
import os
import time
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

PERSONAL_TENANT = "84df9e7f-e9f6-40af-b435-aaaaaaaaaaaa"
CLIENT_ID = "c0ab8ce9-e9a0-42e7-b064-33d422df41f1"


def main():
    from playwright.sync_api import sync_playwright, Error as BrowserError

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", default="secrets/personal-browser")
    parser.add_argument("--output", default="secrets/personal.json")
    parser.add_argument("--timeout", type=int, default=600)
    args = parser.parse_args()
    os.umask(0o077)
    destination = Path(args.output).resolve()
    if destination.exists():
        raise SystemExit("Output already exists. Choose a new --output path.")
    profile = Path(args.profile).resolve()
    profile.mkdir(parents=True, exist_ok=True, mode=0o700)
    captured = {}
    candidates = []

    def response_received(response):
        parsed = urlsplit(response.url)
        if parsed.hostname != "login.microsoftonline.com" or not parsed.path.endswith("/oauth2/v2.0/token"):
            return
        form = parse_qs(response.request.post_data or "")
        if form.get("client_id", [""])[0] != CLIENT_ID:
            return
        if "substrate.office.com/sydney" not in form.get("scope", [""])[0]:
            return
        try:
            body = response.json()
            anchor = form.get("X-AnchorMailbox", [""])[0]
            if not anchor:
                anchor = response.request.headers.get("x-anchormailbox", "")
            if body.get("access_token"):
                candidates.append((body, anchor))
        except Exception:
            pass

    def socket_opened(ws):
        parsed = urlsplit(ws.url)
        if parsed.hostname != "substrate.office.com" or not parsed.path.startswith("/m365Copilot/Chathub/"):
            return
        identity = parsed.path.rsplit("/", 1)[-1].split("@")
        if len(identity) != 2 or identity[1] != PERSONAL_TENANT:
            return
        query = parse_qs(parsed.query)
        token = query.get("access_token", [""])[0]
        if not token:
            return
        captured.update(access_token=token, oid=identity[0],
                        license_type=query.get("licenseType", ["Free"])[0],
                        expires_at=time.time() + 3600)

    with sync_playwright() as pw:
        context = pw.chromium.launch_persistent_context(str(profile), headless=False)
        # Register for every page so redirects and sign-in popups are covered.
        def attach(page):
            page.on("response", response_received)
            page.on("websocket", socket_opened)
        context.on("page", attach)
        for page in context.pages:
            attach(page)
        page = context.pages[0] if context.pages else context.new_page()
        print("Sign in with your PERSONAL Microsoft account, open Chat and send one short test message.")
        print("Do not copy callback URLs or tokens. The export stays on this computer.")
        try:
            page.goto("https://m365.cloud.microsoft/chat", wait_until="domcontentloaded")
            deadline = time.monotonic() + args.timeout
            while not captured and time.monotonic() < deadline:
                page.wait_for_timeout(250)
            if not captured:
                raise SystemExit("No personal chat session captured; sign in and send a message, then try again.")
            for body, anchor in reversed(candidates):
                if body["access_token"] == captured["access_token"]:
                    captured["expires_at"] = time.time() + int(body.get("expires_in", 3600))
                if body.get("refresh_token") and anchor.lower().startswith("oid:" + captured["oid"].lower() + "@"):
                    captured["refresh_token"] = body["refresh_token"]
                    break
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            # Exclusive creation protects an existing export, including symlinks.
            with destination.open("x", encoding="utf-8") as f:
                json.dump(captured, f)
            print("Saved private credential file:", destination)
            if "refresh_token" not in captured:
                print("No matching refresh token observed. Re-run login when this short-lived session expires.")
        except BrowserError:
            raise SystemExit("Browser closed or navigation failed before the export completed. Run the helper again.") from None
        finally:
            try:
                context.close()
            except BrowserError:
                pass


if __name__ == "__main__":
    main()
