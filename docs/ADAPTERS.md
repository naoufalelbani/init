# Agent adapters

`AGENTS.md` is the canonical project contract. `ai-init agents configure` detects known workspace markers, or accepts an explicit comma-separated selection. It only creates files at documented locations. Unmarked existing files are left untouched; files managed by ai-init use delimiters and receive a recoverable `.ai-init.bak` before refresh.

| Agent | Integration | State |
|---|---|---|
| Antigravity | Root `AGENTS.md` | Native support |
| Codex | Root `AGENTS.md` | Native support |
| Claude Code | Root `CLAUDE.md` imports `AGENTS.md` | Generated reference |
| Cursor | `.cursor/rules/ai-init.mdc` | Generated always-applied MDC rule |
| OpenCode | Root `AGENTS.md` | Native support |
| Windsurf | `.windsurf/rules/ai-init.md` with `trigger: always_on` | Generated always-on rule |
| Cline | Root `AGENTS.md` | Native support |
| GitHub Copilot | `.github/copilot-instructions.md` points to `AGENTS.md` | Generated instructions |
| Aider | `.aider.conf.yml` adds `AGENTS.md` as read-only context | Generated only when explicitly selected and no existing config exists |

Useful commands:

```sh
ai-init agents list
ai-init agents configure --path . --adapter claude,cursor
ai-init setup --adapters copilot,aider
```

A detected custom config without ai-init markers is preserved and reported. Adapter-specific references are intentionally brief so that rules remain canonical in `AGENTS.md`.

## Format references

- Antigravity: https://www.antigravity.google/docs/rules/
- Claude Code: https://docs.anthropic.com/en/docs/claude-code/memory
- Cursor: https://docs.cursor.com/context/rules
- Windsurf: https://docs.windsurf.com/windsurf/cascade/memories
- OpenCode: https://opencode.ai/docs/rules/
- Cline: https://docs.cline.bot/customization/cline-rules
- GitHub Copilot: https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/add-custom-instructions/add-repository-instructions
- Aider: https://aider.chat/docs/usage/conventions.html and https://aider.chat/docs/config/aider_conf.html
