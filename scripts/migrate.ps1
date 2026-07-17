param(
    [string]$File = "backend/migrations/0001_init.up.sql"
)

Write-Output "Database migration file hint: $File"
Write-Output "Use goose/atlas/migrate as your SQL migration runner. This project only stores SQL templates."
