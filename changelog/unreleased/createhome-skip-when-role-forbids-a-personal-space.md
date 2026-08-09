Bugfix: Don't ask the gateway for a personal space a role may not have

The proxy asked the gateway to create the user's personal space on every authenticated request. For a user whose role carries no CreateSpaces permission that request can only ever be denied, and the denial was logged at error level, so such a deployment produced one error line and one wasted gateway round-trip per request. The role assigners now record whether the user's role permits a space, and the middleware skips the call when it does not.

https://github.com/opencloud-eu/opencloud/pull/PR_NUMBER
