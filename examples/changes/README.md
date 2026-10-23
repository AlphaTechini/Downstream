# Example changes

These patches are applied to the demo app in `examples/shelf` to make a candidate
commit for Downstream to measure. Each applies with `git apply` from the repository root, and each is an entry in the
`engine-e2e` CI matrix.

`raise-hash-cost.patch` is a planted regression, made for the demo. It raises the PBKDF2
iteration count in `examples/shelf/secret.go` and touches nothing else. In review it reads
as a security improvement: the comment cites OWASP guidance and the number moves toward it.
The cost is not visible in the diff. The same helper checks the secret half of the API
token on every authenticated request, so every route behind `requireAuth` becomes slower,
including routes the diff never mentions.

`clean-change.patch` is a harmless change: `GET /health` also returns the service name. It
touches only that handler and has no performance effect, so a run on it should show no
difference beyond noise.

`remove-list-limit.patch` is a planted regression, made for the demo. **Scripted edit, not
AI-written:** it is the output of one fixed substitution on `examples/shelf/handlers.go`,

```
sed -i 's/^\tmaxListed = 50$/\tmaxListed = 100000/' examples/shelf/handlers.go
```

followed by `git diff`. It removes the page size of `GET /books` in effect: the list returns
every matching book, and `listBooks` also reserves room for the limit on every call. It needs
journeys that create more books than one page holds, so its CI entry uses
`examples/shelf/journeys.thorough.json`. What it changes in a run, measured:
[`evidence/plants/`](../../evidence/plants/).
