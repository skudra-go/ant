## What and why

<!-- What does this change, and why? Link the issue: Closes #123 -->

## Checklist

- [ ] The title follows Conventional Commits (`feat:`, `fix:`, `docs:` ...), since it becomes the squashed commit
- [ ] The branch is rebased on `main` (`git pull --rebase origin main`), with no merge commits
- [ ] Tests cover the change, and at least one fails without it
- [ ] `make fmt`, `make lint` and `make test` pass
- [ ] `make test-pg` passes, if the change reaches the database (and the other dialects' targets, if it touches them)
- [ ] `docs/api.txt` is updated with `go test ./tests/api -update`, if the exported API changed on purpose
- [ ] Docs are updated here or in [ant-docs](https://github.com/skudra-go/ant-docs), if behaviour changed
