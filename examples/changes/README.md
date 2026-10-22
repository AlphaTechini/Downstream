# Example changes

These two patches are applied to the demo app in `examples/shelf` to make a candidate
commit for Downstream to measure. Both apply with `git apply` from the repository root.

`raise-hash-cost.patch` is a planted regression, made for the demo. It raises the PBKDF2
iteration count in `examples/shelf/secret.go` and touches nothing else. In review it reads
as a security improvement: the comment cites OWASP guidance and the number moves toward it.
The cost is not visible in the diff. The same helper checks the secret half of the API
token on every authenticated request, so every route behind `requireAuth` becomes slower,
including routes the diff never mentions.

`clean-change.patch` is a harmless change: `GET /health` also returns the service name. It
touches only that handler and has no performance effect, so a run on it should show no
difference beyond noise.
