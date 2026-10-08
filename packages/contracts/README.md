# @ideavault/contracts

`openapi.json` is the API contract between `apps/api` and `apps/web`. It is **generated** from the Go route registry and request/response types — do not edit it by hand.

```bash
make contracts   # regenerates openapi.json and apps/web/types/api.generated.ts
```

CI fails if either file is stale.
