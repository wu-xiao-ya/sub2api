# Console visual refresh: candidate acceptance

Date: 2026-09-13
Baseline: 21159fb8d3e4efaeadb57980a44902d6f14add43
Scope: frontend presentation, bundled brand resources, tests and candidate-only CI.
Production deployment is NOT part of this change.

## Presentation contract

- Neutral light/dark surfaces, restrained teal interaction state, 6px controls and 8px repeated cards.
- Existing Vue/Tailwind/Chart.js stack and API contracts are unchanged.
- Dashboard, model plaza, keys, usage, subscriptions and both channel-status modes use the common console shell.
- Usage cost precision and balance/subscription labels, Fast tier, TTFT, CSV and filter logic stay in their original owners.
- Subscription priority and balance top-up handlers are unchanged. V1 remains the default channel-status mode.
- Admin tables and dialogs inherit common controls; no admin business workflow is rearranged.
- Responsive distribution charts now stack at constrained widths; sticky table cells use the neutral dark palette.

## Brand provenance

The single registry is frontend/src/utils/brandRegistry.ts. PlatformIcon, ModelIcon and ProviderIcon are compatibility wrappers.
Model families resolve independently of the upstream transport/platform. Unknown custom names use a neutral cube, not an invented logo.
All image bytes are bundled; no image CDN requests or React icon imports are added at runtime.

The machine-readable manifest in frontend/src/assets/brands/sources.json records each file, SHA-256, acquisition URL and verification status.
SVG hashes normalize CRLF and trim surrounding whitespace; PNG hashes cover raw bytes.
SVGs are parsed and checked for scripts, event handlers, foreign objects, external references and malformed content.

Core platform verification:

| Brand | Reference and disposition |
| --- | --- |
| OpenAI | developers.openai.com favicon: logomark glyph checked; monochrome library variant retained |
| Claude | claude.ai favicon: orange product mark checked |
| Gemini | gemini.google/images/spark_4c.png: colored sparkle checked |
| Grok | Official Grok AI App Store listing, ID 6670324846, seller X Corp., seller website x.ai: product glyph checked; not the xAI company glyph |
| Antigravity | antigravity.google/assets/image/antigravity-logo.png: official color resource bundled |
| DeepSeek | deepseek.com favicon and site logo: product mark checked |
| Kimi | kimi.com/favicon-light.ico: K product mark checked; black backing prevents white glyph disappearing |
| GLM | bigmodel.cn/static/images/favicon.png: official current resource bundled, replacing older dotted Zhipu mark |
| Qwen | qwen.ai linked official alicdn favicon: current solid blue mark bundled, replacing old outline variant |
| MiniMax | minimax.io linked official logo: waveform symbol checked, not Hailuo |
| MiMo | mimo.xiaomi.com linked official wordmark: MiMo lettering checked; compact library lockup retained, not Xiaomi generic logo |
| Hunyuan | hunyuan.tencent.com linked official logo.svg: colored product symbol checked |

Library artwork is from @lobehub/icons-static-svg 1.95.0; the included MIT notice applies to the library, not a trademark license.
Official marks remain their owners' property. Use is identification only and must not imply endorsement.
The other 17 legacy model-family recognizers are preserved, but their additional official-source revalidation remains marked pending in the manifest. Do not describe those as newly verified official assets.

## Reproducible local acceptance

From frontend/, run node scripts/console-preview.mjs.
Open http://127.0.0.1:5194/__preview. It seeds a synthetic user and only uses local in-memory fixtures on port 5195.
There are no production credentials, real user data or callable upstream model keys. /v1/ calls are rejected.

Run node scripts/console-qa.mjs with Playwright installed, or set PLAYWRIGHT_PACKAGE_JSON to a runtime package.json that can resolve Playwright.
Set CHROME_PATH when using a locally installed Chrome. CONSOLE_QA_OUTPUT selects the artifact directory.
The default run covers 7 views x 4 widths (375/768/1440/1920) x 2 themes, PNG screenshots, page overflow, broken images, canvas pixels and JS exceptions.
It also exercises subscription preference persistence, billing priority, V1/V2 switching, model filtering/empty state, CSV download and key creation.
The test uses only synthetic data; it does not establish real gateway/billing behavior.

## Test baseline

- Related component/unit suite: 157 passing before the additional compiled-CSS regression test.
- Vue typecheck: passed.
- Changed Vue/TS/JS ESLint check: no errors (local pnpm parser resolution required NODE_PATH).
- Broad suite initial run: 1,333 passed / 38 failed. Five assertions were stale visual expectations for SVG elements and large corner radii and were updated with the new intended contract.
- The remaining 33 failures were reproduced by extracting the unchanged baseline to an isolated directory and rerunning the same five files. They cover rollback timeout expectations, auth defaults, locale message compilation, bulk-account-edit mocks and group-column expectations. They are not silently skipped or reported as passing.
- Those baseline issues still need their own review before production sign-off, especially locale compilation and admin account editing. This candidate workflow runs the changed-surface suite rather than falsely representing the entire repository as green.

## Candidate build and release boundary

.github/workflows/console-candidate.yml runs on this branch's frontend changes or manual dispatch.
It checks types/tests, builds the complete embedded frontend image, and starts an empty isolated Postgres/Redis/application/Nginx stack on the GitHub runner.
Root and /starlightai/ HTML, health, public settings and JS/CSS assets must pass before publishing ghcr.io/<owner>/sub2api:console-<full SHA>.
It never changes the mutable production tag and never connects to a production host/database.
The temporary CI containers are removed by a trap and their logs/pages report are uploaded as evidence.

Before any production authorization: inspect the candidate evidence, review unresolved baseline/secondary-brand items, verify admin finance/dialog views against production-shaped data and run the existing API/stream/image/subscription release checks.
Only then use the established candidate validation, entry switch and connection-drain procedure. Do not combine with cleanup or database changes.
