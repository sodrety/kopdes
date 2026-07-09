<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **kopdes** (944 symbols, 3493 relationships, 81 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/kopdes/context` | Codebase overview, check index freshness |
| `gitnexus://repo/kopdes/clusters` | All functional areas |
| `gitnexus://repo/kopdes/processes` | All execution flows |
| `gitnexus://repo/kopdes/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->

## Internationalization

- All user-facing browser copy must go through `internal/app/i18n.go` using `t .Lang "key"` in templates or `translate(languageFromRequest(c), "key")` in Go handlers.
- Add both English (`en`) and Bahasa Indonesia (`id`) translations for every new key in the same change. This includes labels, headings, table headers, empty states, button text, placeholders, aria/title text, toast messages, client-side fallback messages, report text, and export-triggering UI copy.
- Keep stable internal enum values and route/API identifiers in English; translate only their display labels, for example with `status_*`, `deposit`, `withdrawal`, and Simpanan/Pinjaman/Angsuran UI keys.
- Do not add hardcoded English or Bahasa strings inside templates or inline JavaScript unless the text is a brand/legal name or a language autonym such as `English` or `Bahasa Indonesia`.
