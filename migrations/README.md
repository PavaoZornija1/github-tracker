# Migrations

Local and demo environments use Ent `Schema.Create` on API/worker startup when `APP_ENV` is empty, `development`, `local`, `dev`, or `test`.

**Production (`APP_ENV=production`) skips auto-migrate.** Apply versioned migrations before rolling out new schema.

## Next step: Atlas

Wire [Ent versioned migrations with Atlas](https://entgo.io/docs/versioned-migrations) before multi-instance production deploys:

1. Generate SQL diffs from Ent schema into this directory.
2. Apply with `atlas migrate apply` in CI/CD (or startup init job).
3. Keep `Schema.Create` gated off in production.

This directory is a placeholder until Atlas is wired.
