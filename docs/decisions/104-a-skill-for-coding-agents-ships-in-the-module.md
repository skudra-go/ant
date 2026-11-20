---
id: 104
title: "A skill for coding agents ships in the module"
area: tooling
status: active
tags: [skill, SKILL.md, llms.txt, claude-plugin, agents, doccheck]
issues: []
related: []
changed_by: []
code: [skills/ant/SKILL.md, .claude-plugin]
tests: [tests/api/skill_test.go]
---

# 104. A skill for coding agents ships in the module

**In short:** The repo ships skills/ant/SKILL.md and a Claude plugin manifest so agents learn ant's API; tests/api fails if the skill names a missing API.

## Decision

Models trained before ant existed write GORM when asked for ant code: string conditions, `Preload`, `AutoMigrate`. The docs site serves itself as Markdown (`llms.txt`, `llms-full.txt` and a `.md` copy of each page), which an assistant can read if someone points it there.

Decision: the repo carries a skill, `skills/ant/SKILL.md`: the rules an assistant gets wrong when it guesses, in a few hundred lines, with links to the Markdown docs for the rest. It lives in this repo rather than in ant-docs so it is versioned with the code and lands in the module cache with every `go get`, where the package doc points to it. The same directory is a Claude Code plugin (`.claude-plugin/`) and the layout `npx skills add` reads, so installing it is one command for most agents. Nothing installs it by itself: a library can't write into a user's agent settings, and shouldn't. `tests/api` fails when the skill names a function or method the API no longer has; its samples are type-checked with ant-docs' doccheck, by hand, since that tool lives in the other repo.
