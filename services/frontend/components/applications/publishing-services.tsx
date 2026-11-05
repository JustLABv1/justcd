"use client"
import { PublishingSection } from "./publishing-fields"
import { FeatureCard } from "@/components/feature-card"
import { FormField } from "@/components/ui-kit"
import { Disclosure } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import type { ServiceOptions, ServiceSelection } from "@/lib/publishing"

const dbVariables = ["DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"]
const identifier = /^[a-z][a-z0-9_]{0,62}$/
const envName = /^[A-Za-z_][A-Za-z0-9_]*$/

function databaseProblems(
  options: ServiceOptions,
  value: ServiceSelection,
  password?: string
) {
  const problems: { name?: string; username?: string; password?: string } = {}
  const database = options.database
  if (database?.allowConfigure) {
    const name = value.databaseConfig?.name ?? database.databaseName ?? "app"
    const username =
      value.databaseConfig?.username ?? database.username ?? "app"
    if (!identifier.test(name))
      problems.name =
        "Use lowercase letters, numbers and underscores, starting with a letter."
    if (!identifier.test(username))
      problems.username =
        "Use lowercase letters, numbers and underscores, starting with a letter."
  }
  if (password && (password.length < 16 || password.length > 1024))
    problems.password = "Use between 16 and 1024 characters, or leave empty."
  return problems
}

function mappingProblem(
  rows: { name: string; value: string }[],
  needsValue: boolean
) {
  return rows.some(
    (row) => !envName.test(row.name) || (needsValue && !row.value.trim())
  )
}

/**
 * Returns the first issue that blocks publishing. The detail controls live in
 * side sheets outside the form, so the browser cannot validate them itself.
 */
export function servicesProblem(
  options: ServiceOptions | undefined,
  value: ServiceSelection,
  password?: string
): string | null {
  if (!options) return null
  if (value.database && options.databaseName) {
    const problems = databaseProblems(options, value, password)
    const first = problems.name ?? problems.username ?? problems.password
    if (first) return `Database: ${first}`
  }
  for (const binding of options.vaultSecrets ?? []) {
    const required =
      value.database && options.databaseVaultSecret === binding.id
    if (!required && !value.vaultSecrets.includes(binding.id)) continue
    const rows = (
      value.vaultConfig?.[binding.id]?.envs ??
      binding.envs ??
      []
    ).map((e) => ({ name: e.name, value: e.key }))
    if (binding.allowEnvMapping && mappingProblem(rows, true))
      return `${binding.name}: every environment variable needs a valid name and a Secret key.`
  }
  if (
    options.allowEnvironment &&
    mappingProblem(value.environment ?? [], false)
  )
    return "Additional environment variables: every entry needs a valid name."
  return null
}

export function PublishingServicesPicker({
  options,
  value,
  onChange,
  password,
  onPasswordChange,
}: {
  /** Kept for the settings preview; the card layout is identical. */
  preview?: boolean
  options?: ServiceOptions
  value: ServiceSelection
  onChange: (value: ServiceSelection) => void
  password?: string
  onPasswordChange?: (value: string) => void
}) {
  if (
    !options?.databaseName &&
    !options?.vaultSecrets?.length &&
    !options?.allowEnvironment
  )
    return null
  const database = options.database
  const patchDatabase = (
    patch: NonNullable<ServiceSelection["databaseConfig"]>
  ) =>
    onChange({
      ...value,
      databaseConfig: { ...value.databaseConfig, ...patch },
    })
  const dbProblems = databaseProblems(options, value, password)
  const dbName = value.databaseConfig?.name ?? database?.databaseName ?? "app"
  const dbUser = value.databaseConfig?.username ?? database?.username ?? "app"
  return (
    <PublishingSection
      title="Database and application secrets"
      description="Turn on what your app needs. Select a card to adjust its settings; infrastructure and access boundaries stay controlled by the profile."
    >
      <div className="grid gap-3">
        {options.databaseName && (
          <FeatureCard
            id="publish-with-database"
            title={`Create ${options.databaseName}`}
            description="Provision a dedicated PostgreSQL cluster. Database data and credentials are retained when the app is deleted."
            enabled={value.database}
            onEnabledChange={(enabled) =>
              onChange({
                ...value,
                database: enabled,
                ...(!enabled ? { databaseConfig: undefined } : {}),
              })
            }
            summary={`Database ${dbName} · user ${dbUser} · ${password ? "your password" : "generated password"}`}
            attention={
              Object.keys(dbProblems).length ? "Needs attention" : undefined
            }
            sheetTitle={`${options.databaseName} settings`}
            sheetDescription="Names and credentials for this app's database."
          >
            {database?.allowConfigure ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField
                  label="Database name"
                  htmlFor="app-database-name"
                  hint="Lowercase letters, numbers and underscores."
                  error={dbProblems.name}
                >
                  <Input
                    id="app-database-name"
                    aria-invalid={!!dbProblems.name}
                    value={dbName}
                    onChange={(e) => patchDatabase({ name: e.target.value })}
                  />
                </FormField>
                <FormField
                  label="Application username"
                  htmlFor="app-database-user"
                  error={dbProblems.username}
                >
                  <Input
                    id="app-database-user"
                    aria-invalid={!!dbProblems.username}
                    value={dbUser}
                    onChange={(e) =>
                      patchDatabase({ username: e.target.value })
                    }
                  />
                </FormField>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                Database name and username are configured by your administrator.
              </p>
            )}
            {database?.credentialMode === "generated" ? (
              <>
                <p className="text-sm text-muted-foreground">
                  JustCD generates a password and creates the restricted
                  database user. Credentials go directly into a retained
                  Kubernetes Secret during deployment.
                </p>
                {database.allowCustomPassword && onPasswordChange && (
                  <FormField
                    label="Database password (optional)"
                    htmlFor="app-database-password"
                    hint="Leave empty to generate one. At least 16 characters. Never saved in drafts, Git or previews."
                    error={dbProblems.password}
                  >
                    <Input
                      id="app-database-password"
                      type="password"
                      autoComplete="new-password"
                      aria-invalid={!!dbProblems.password}
                      maxLength={1024}
                      value={password ?? ""}
                      onChange={(e) => onPasswordChange(e.target.value)}
                    />
                  </FormField>
                )}
              </>
            ) : (
              <p className="text-sm text-muted-foreground">
                Credentials and bootstrap SQL are supplied by your platform or
                Vault. Change their values at that source.
              </p>
            )}
            <Disclosure summary="Environment variable names">
              <div className="grid gap-3 sm:grid-cols-2">
                {dbVariables.map((key) => (
                  <FormField
                    key={key}
                    label={key.replace("DB_", "")}
                    htmlFor={`app-db-env-${key}`}
                  >
                    <Input
                      id={`app-db-env-${key}`}
                      value={
                        value.databaseConfig?.envNames?.[key] ??
                        database?.envNames?.[key] ??
                        key
                      }
                      disabled={!database?.allowEnvMapping}
                      onChange={(e) =>
                        patchDatabase({
                          envNames: {
                            ...database?.envNames,
                            ...value.databaseConfig?.envNames,
                            [key]: e.target.value,
                          },
                        })
                      }
                    />
                  </FormField>
                ))}
              </div>
              <p className="mt-3 text-xs text-muted-foreground">
                Use the names your app expects. Leave a mapping empty to omit
                it.
              </p>
            </Disclosure>
          </FeatureCard>
        )}
        {options.vaultSecrets?.map((binding) => {
          const required =
            value.database && options.databaseVaultSecret === binding.id
          const selected = required || value.vaultSecrets.includes(binding.id)
          const config = value.vaultConfig?.[binding.id]
          const patch = (
            next: NonNullable<ServiceSelection["vaultConfig"]>[string]
          ) =>
            onChange({
              ...value,
              vaultConfig: {
                ...value.vaultConfig,
                [binding.id]: { ...config, ...next },
              },
            })
          const rows = (config?.envs ?? binding.envs ?? []).map((e) => ({
            name: e.name,
            value: e.key,
          }))
          const editable =
            !!binding.allowPathSuffix || !!binding.allowEnvMapping
          const invalid = binding.allowEnvMapping && mappingProblem(rows, true)
          return (
            <FeatureCard
              key={binding.id}
              id={`publish-vault-${binding.id}`}
              title={binding.name}
              description={
                required
                  ? "Required for the database. Values are synchronized from Vault."
                  : "Synchronize a secret from the approved Vault location."
              }
              enabled={selected}
              switchDisabled={required}
              summary={`${binding.path ?? "Approved location"}${config?.pathSuffix ? `/${config.pathSuffix}` : ""} · ${rows.length} environment variable${rows.length === 1 ? "" : "s"}`}
              attention={invalid ? "Needs attention" : undefined}
              onEnabledChange={(checked) => {
                const configs = { ...value.vaultConfig }
                if (!checked) delete configs[binding.id]
                onChange({
                  ...value,
                  vaultConfig: configs,
                  vaultSecrets: checked
                    ? [...value.vaultSecrets, binding.id]
                    : value.vaultSecrets.filter((id) => id !== binding.id),
                })
              }}
              sheetTitle={binding.name}
              sheetDescription="Choose where the secret is read from and which environment variables receive its keys."
            >
              {editable ? (
                <>
                  {binding.allowPathSuffix ? (
                    <FormField
                      label="Secret path within the approved location"
                      htmlFor={`vault-suffix-${binding.id}`}
                      hint={`Approved location: ${binding.path}. Use a relative path such as reports/s3; parent paths are not allowed.`}
                    >
                      <Input
                        id={`vault-suffix-${binding.id}`}
                        value={config?.pathSuffix ?? ""}
                        onChange={(e) => patch({ pathSuffix: e.target.value })}
                        placeholder="reports/s3"
                      />
                    </FormField>
                  ) : (
                    <p className="text-sm text-muted-foreground">
                      Vault location is fixed by your administrator.
                    </p>
                  )}
                  {binding.allowEnvMapping ? (
                    <Mappings
                      id={`vault-mapping-${binding.id}`}
                      rows={rows}
                      valueLabel="Secret key"
                      onChange={(next) =>
                        patch({
                          envs: next.map((e) => ({
                            name: e.name,
                            key: e.value,
                          })),
                        })
                      }
                    />
                  ) : (
                    <p className="text-sm text-muted-foreground">
                      Environment mappings:{" "}
                      {(binding.envs ?? [])
                        .map((e) => `${e.name} ← ${e.key}`)
                        .join(", ") ||
                        "Secret only; no environment variables"}{" "}
                      (configured by your administrator).
                    </p>
                  )}
                </>
              ) : undefined}
            </FeatureCard>
          )
        })}
        {options.allowEnvironment && (
          <FeatureCard
            id="publish-environment"
            title="Additional environment variables"
            description="Non-secret values only. Use a Vault mapping for passwords, access keys and encryption keys. These values are stored in Git and browser drafts."
            summary={
              (value.environment ?? []).length
                ? (value.environment ?? []).map((e) => e.name || "…").join(", ")
                : "None added"
            }
            attention={
              mappingProblem(value.environment ?? [], false)
                ? "Needs attention"
                : undefined
            }
            sheetTitle="Additional environment variables"
          >
            <Mappings
              id="app-environment"
              rows={value.environment ?? []}
              valueLabel="Value"
              onChange={(environment) => onChange({ ...value, environment })}
            />
          </FeatureCard>
        )}
      </div>
    </PublishingSection>
  )
}
function Mappings({
  id,
  rows,
  valueLabel,
  onChange,
}: {
  id: string
  rows: { name: string; value: string }[]
  valueLabel: string
  onChange: (rows: { name: string; value: string }[]) => void
}) {
  return (
    <div className="space-y-3">
      {rows.map((row, i) => (
        <div
          key={i}
          className="grid items-end gap-3 sm:grid-cols-[1fr_1fr_auto]"
        >
          <FormField label="Environment variable" htmlFor={`${id}-name-${i}`}>
            <Input
              id={`${id}-name-${i}`}
              aria-invalid={!envName.test(row.name)}
              value={row.name}
              onChange={(e) =>
                onChange(
                  rows.map((r, j) =>
                    j === i ? { ...r, name: e.target.value } : r
                  )
                )
              }
            />
          </FormField>
          <FormField label={valueLabel} htmlFor={`${id}-value-${i}`}>
            <Input
              id={`${id}-value-${i}`}
              aria-invalid={valueLabel === "Secret key" && !row.value.trim()}
              value={row.value}
              onChange={(e) =>
                onChange(
                  rows.map((r, j) =>
                    j === i ? { ...r, value: e.target.value } : r
                  )
                )
              }
            />
          </FormField>
          <Button
            type="button"
            variant="destructive"
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
          >
            Remove
          </Button>
        </div>
      ))}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={rows.length >= 30}
        onClick={() => onChange([...rows, { name: "", value: "" }])}
      >
        Add environment variable
      </Button>
    </div>
  )
}
