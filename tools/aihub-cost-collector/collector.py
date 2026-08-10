#!/usr/bin/env python3
"""Read AIHub's rendered usage page and persist a minimal cost snapshot."""

from __future__ import annotations

import json
import logging
import os
import re
import signal
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from selenium import webdriver
from selenium.common.exceptions import TimeoutException, WebDriverException
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.support import expected_conditions as ec
from selenium.webdriver.support.ui import WebDriverWait


LOGGER = logging.getLogger("aihub-cost-collector")
RUNNING = True

SELENIUM_URL = os.environ.get("SELENIUM_URL", "http://browser:4444/wd/hub")
AIHUB_USAGE_URL = os.environ.get("AIHUB_USAGE_URL", "https://aihub.top/usage")
INTERVAL_SECONDS = max(int(os.environ.get("COLLECT_INTERVAL_SECONDS", "900")), 60)
PAGE_TIMEOUT_SECONDS = max(int(os.environ.get("PAGE_TIMEOUT_SECONDS", "45")), 10)
SNAPSHOT_DIR = Path(os.environ.get("SNAPSHOT_DIR", "/data"))


def stop(_signum: int, _frame: Any) -> None:
    global RUNNING
    RUNNING = False


def now_utc() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temporary = path.with_suffix(f"{path.suffix}.tmp")
    temporary.write_text(
        json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n",
        encoding="utf-8",
    )
    temporary.replace(path)


def append_history(payload: dict[str, Any]) -> None:
    history = SNAPSHOT_DIR / "history" / f"{datetime.now().date().isoformat()}.jsonl"
    history.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with history.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n")


def set_status(state: str, message: str, **details: Any) -> None:
    payload: dict[str, Any] = {
        "state": state,
        "message": message,
        "updated_at": now_utc(),
        **details,
    }
    write_json(SNAPSHOT_DIR / "status.json", payload)


def make_driver() -> webdriver.Remote:
    options = Options()
    # AIHub may leave third-party resources open behind Cloudflare. Do not let
    # WebDriver's navigation wait close the interactive noVNC browser session.
    options.page_load_strategy = "none"
    options.add_argument("--window-size=1440,1200")
    options.add_argument("--lang=zh-CN")
    options.add_argument("--user-data-dir=/home/seluser/aihub-profile")
    options.add_argument("--disable-dev-shm-usage")
    options.add_argument("--no-first-run")
    options.add_argument("--no-default-browser-check")
    driver = webdriver.Remote(command_executor=SELENIUM_URL, options=options)
    driver.set_page_load_timeout(PAGE_TIMEOUT_SECONDS)
    return driver


def normalized_text(element: Any) -> str:
    return re.sub(r"\s+", " ", element.text or "").strip()


def first_visible_button(driver: webdriver.Remote, labels: tuple[str, ...]) -> Any | None:
    for label in labels:
        buttons = driver.find_elements(
            By.XPATH,
            f"//button[normalize-space(.)={json.dumps(label, ensure_ascii=False)}]",
        )
        for button in buttons:
            if button.is_displayed() and button.is_enabled():
                return button
    return None


def select_today_and_refresh(driver: webdriver.Remote, wait: WebDriverWait) -> bool:
    main = wait.until(ec.presence_of_element_located((By.TAG_NAME, "main")))
    main_text = normalized_text(main)
    if "总消费" not in main_text and "Total Cost" not in main_text:
        return False

    range_button = driver.find_element(
        By.XPATH,
        "//*[normalize-space(.)='时间范围:']/following::button[1]"
        "|//*[normalize-space(.)='Time range:']/following::button[1]",
    )
    range_label = normalized_text(range_button)
    if range_label not in {"今天", "Today"}:
        range_button.click()
        today_button = first_visible_button(driver, ("今天", "Today"))
        if today_button is None:
            raise RuntimeError("today range option was not found")
        today_button.click()
        time.sleep(0.5)

    refresh_button = first_visible_button(driver, ("刷新", "Refresh"))
    if refresh_button is None:
        raise RuntimeError("refresh button was not found")
    refresh_button.click()
    time.sleep(1.2)
    return True


def extract_amount(text: str, labels: tuple[str, ...]) -> float | None:
    for label in labels:
        pattern = rf"{re.escape(label)}\s*\$?\s*([0-9][0-9,]*(?:\.[0-9]+)?)"
        match = re.search(pattern, text, flags=re.IGNORECASE)
        if match:
            return float(match.group(1).replace(",", ""))
    return None


def extract_count(text: str, labels: tuple[str, ...]) -> int | None:
    for label in labels:
        pattern = rf"{re.escape(label)}\s*([0-9][0-9,]*)"
        match = re.search(pattern, text, flags=re.IGNORECASE)
        if match:
            return int(match.group(1).replace(",", ""))
    return None


def extract_summary(driver: webdriver.Remote) -> dict[str, Any]:
    main_text = driver.find_element(By.TAG_NAME, "main").text
    total_cost = extract_amount(main_text, ("总消费", "Total Cost"))
    standard_cost = extract_amount(main_text, ("标准", "Standard"))
    requests = extract_count(main_text, ("总请求数", "Total Requests"))
    if total_cost is None or standard_cost is None or requests is None:
        raise RuntimeError("AIHub usage page did not contain a complete summary")

    tables = driver.execute_script(
        """
        return Array.from(document.querySelectorAll('table'))
          .map((table) => table.innerText.trim())
          .filter((text) => /^(模型|Model|分组|Group|端点|Endpoint)\\t/m.test(text));
        """
    )
    return {
        "source": "aihub",
        "period": "today",
        "captured_at": now_utc(),
        "requests": requests,
        "actual_cost": total_cost,
        "standard_cost": standard_cost,
        "tables": tables,
    }


def login_required(driver: webdriver.Remote) -> bool:
    current_url = driver.current_url.lower()
    page_text = driver.find_element(By.TAG_NAME, "body").text
    return "/login" in current_url or "登录" in page_text or "Sign in" in page_text


def challenge_required(driver: webdriver.Remote) -> bool:
    page_text = driver.find_element(By.TAG_NAME, "body").text.lower()
    return any(
        marker in page_text
        for marker in (
            "just a moment",
            "enable javascript and cookies",
            "请稍候",
            "验证您是真人",
        )
    )


def capture(driver: webdriver.Remote) -> bool:
    wait = WebDriverWait(driver, PAGE_TIMEOUT_SECONDS)
    try:
        driver.get(AIHUB_USAGE_URL)
    except TimeoutException:
        # Cloudflare or a slow third-party asset can keep the load event open.
        # The rendered DOM is still useful for detecting login/challenge states.
        LOGGER.info("AIHub page load timed out; stopping navigation and inspecting the DOM")
        try:
            driver.execute_script("window.stop();")
        except WebDriverException:
            pass

    try:
        WebDriverWait(driver, min(PAGE_TIMEOUT_SECONDS, 10)).until(
            lambda current_driver: (
                current_driver.current_url != "about:blank"
                and normalized_text(current_driver.find_element(By.TAG_NAME, "body"))
            )
        )
    except TimeoutException:
        set_status(
            "page_loading",
            "AIHub is still loading in the private noVNC browser.",
            usage_url=AIHUB_USAGE_URL,
        )
        return False

    if login_required(driver):
        set_status(
            "login_required",
            "Open the private noVNC desktop and sign in to AIHub.",
            usage_url=AIHUB_USAGE_URL,
        )
        return False

    if challenge_required(driver):
        set_status(
            "cloudflare_required",
            "Complete the browser verification in the private noVNC desktop.",
            usage_url=driver.current_url,
        )
        return False

    if not driver.find_elements(By.TAG_NAME, "main"):
        set_status(
            "page_loading",
            "AIHub has not rendered its usage dashboard yet. Check the private noVNC browser.",
            usage_url=driver.current_url,
        )
        return False

    if not select_today_and_refresh(driver, wait):
        set_status("page_loading", "AIHub usage dashboard is still rendering.", usage_url=driver.current_url)
        return False

    snapshot = extract_summary(driver)
    write_json(SNAPSHOT_DIR / "latest.json", snapshot)
    append_history(snapshot)
    set_status(
        "ok",
        "Latest AIHub actual cost snapshot was captured.",
        captured_at=snapshot["captured_at"],
        actual_cost=snapshot["actual_cost"],
    )
    LOGGER.info(
        "captured AIHub snapshot: requests=%s actual_cost=%s standard_cost=%s",
        snapshot["requests"],
        snapshot["actual_cost"],
        snapshot["standard_cost"],
    )
    return True


def wait_or_stop(seconds: int) -> None:
    deadline = time.monotonic() + seconds
    while RUNNING and time.monotonic() < deadline:
        time.sleep(min(1.0, deadline - time.monotonic()))


def main() -> int:
    os.umask(0o077)
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    SNAPSHOT_DIR.mkdir(mode=0o700, parents=True, exist_ok=True)
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)

    driver: webdriver.Remote | None = None
    while RUNNING:
        try:
            if driver is None:
                set_status("starting", "Waiting for the dedicated browser.")
                driver = make_driver()
            captured = capture(driver)
            wait_or_stop(INTERVAL_SECONDS if captured else 30)
        except (TimeoutException, WebDriverException, RuntimeError) as exc:
            LOGGER.warning("capture failed: %s", exc)
            set_status("error", str(exc))
            if driver is not None:
                try:
                    driver.quit()
                except WebDriverException:
                    pass
                driver = None
            wait_or_stop(60)
        except Exception:
            LOGGER.exception("unexpected collector error")
            set_status("error", "Unexpected collector error. Check container logs.")
            if driver is not None:
                try:
                    driver.quit()
                except WebDriverException:
                    pass
                driver = None
            wait_or_stop(60)

    if driver is not None:
        try:
            driver.quit()
        except WebDriverException:
            pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
