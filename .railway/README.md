# Railway configuration

This separate Go module pins the Railway IaC SDK to v0.3.0. It is deployment
tooling and is excluded from the service's build and dependencies. The Go authoring
API is beta; pin updates must include graph inspection and a fresh deployment plan.

The named partial `morsecode-audio` owns only this service in the linked project.
Before applying, review the project/service names and target environment. Keep the
partial name stable after its first use. It declares no databases, volumes or buckets.

The user performs deployment using the [runbook](../docs/railway-deployment.md).
No Railway login, plan, apply or hosted deployment has been performed here.
Railway's current IaC flow requires CLI evaluation; deploying a repository alone
does not apply `.railway/railway.go` settings. The runbook also lists UI settings.
