Bugfix: Stop asking the gateway to create a home on every request

The proxy's CreateHome middleware issued a gRPC CreateHome call for every
authenticated request on a protected route. The answer is stable per user —
either the home exists or the user's role has no CreateSpaces permission — so
all but the first call per user could only repeat an answer already known.
For a user whose role cannot create a home the denial was additionally logged
at error level on every request. The middleware now remembers for five minutes
that it asked.

https://github.com/opencloud-eu/opencloud/pull/PR_NUMBER
