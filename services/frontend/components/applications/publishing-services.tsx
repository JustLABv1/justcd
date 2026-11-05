"use client"
import {
  PublishingCheckbox,
  PublishingSection,
  PublishingCard,
} from "./publishing-fields"
import { FormField } from "@/components/ui-kit"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import type { ServiceOptions, ServiceSelection } from "@/lib/publishing"

const dbVariables = ["DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"]
export function PublishingServicesPicker({
  options,
  value,
  onChange,
  password,
  onPasswordChange,
  preview = false,
}: {
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
  return (
    <PublishingSection
      title="Database and application secrets"
      description="Choose services and configure the app settings your administrator permits. Infrastructure and access boundaries remain controlled by the profile."
    >
      {options.databaseName && (
        <div className="space-y-3">
          <PublishingCheckbox
            id="publish-with-database"
            checked={value.database}
            onCheckedChange={(enabled) =>
              onChange({
                ...value,
                database: enabled,
                ...(!enabled ? { databaseConfig: undefined } : {}),
              })
            }
            title={`Create ${options.databaseName}`}
            description="Provision a dedicated PostgreSQL cluster. Database data and credentials are retained when the app is deleted."
          />
          {value.database && (
            <PublishingCard contentClassName="space-y-4">
              {database?.allowConfigure ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField
                    label="Database name"
                    htmlFor="app-database-name"
                    hint="Lowercase letters, numbers and underscores."
                  >
                    <Input
                      id="app-database-name"
                      required={!preview}
                      pattern={preview ? undefined : "[a-z][a-z0-9_]{0,62}"}
                      value={
                        value.databaseConfig?.name ??
                        database.databaseName ??
                        "app"
                      }
                      onChange={(e) => patchDatabase({ name: e.target.value })}
                    />
                  </FormField>
                  <FormField
                    label="Application username"
                    htmlFor="app-database-user"
                  >
                    <Input
                      id="app-database-user"
                      required={!preview}
                      pattern={preview ? undefined : "[a-z][a-z0-9_]{0,62}"}
                      value={
                        value.databaseConfig?.username ??
                        database.username ??
                        "app"
                      }
                      onChange={(e) =>
                        patchDatabase({ username: e.target.value })
                      }
                    />
                  </FormField>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">
                  Database name and username are configured by your
                  administrator.
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
                    >
                      <Input
                        id="app-database-password"
                        type="password"
                        autoComplete="new-password"
                        minLength={16}
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
              <div className="grid gap-3 sm:grid-cols-2">
                {dbVariables.map((key) => (
                  <FormField
                    key={key}
                    label={`${key.replace("DB_", "")} environment variable`}
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
              <p className="text-xs text-muted-foreground">
                Use the names your app expects. Leave a mapping empty to omit
                it.
              </p>
            </PublishingCard>
          )}
        </div>
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
        return (
          <div key={binding.id} className="space-y-3">
            <PublishingCheckbox
              id={`publish-vault-${binding.id}`}
              checked={selected}
              disabled={required}
              title={binding.name}
              description={
                required
                  ? "Required for the database. Values are synchronized from Vault."
                  : "Synchronize a secret from the approved Vault location."
              }
              onCheckedChange={(checked) => {
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
            />
            {selected && (
              <PublishingCard contentClassName="space-y-4">
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
                    preview={preview}
                    id={`vault-mapping-${binding.id}`}
                    rows={(config?.envs ?? binding.envs ?? []).map((e) => ({
                      name: e.name,
                      value: e.key,
                    }))}
                    valueLabel="Secret key"
                    onChange={(rows) =>
                      patch({
                        envs: rows.map((e) => ({ name: e.name, key: e.value })),
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
              </PublishingCard>
            )}
          </div>
        )
      })}
      {options.allowEnvironment && (
        <PublishingCard contentClassName="space-y-3">
          <h3 className="text-sm font-medium">
            Additional environment variables
          </h3>
          <p className="text-xs text-muted-foreground">
            Non-secret values only. Use a Vault mapping for passwords, access
            keys and encryption keys. These values are stored in Git and browser
            drafts.
          </p>
          <Mappings
            preview={preview}
            id="app-environment"
            rows={value.environment ?? []}
            valueLabel="Value"
            onChange={(environment) => onChange({ ...value, environment })}
          />
        </PublishingCard>
      )}
    </PublishingSection>
  )
}
function Mappings({
  id,
  rows,
  valueLabel,
  onChange,
  preview,
}: {
  preview: boolean
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
              required={!preview}
              pattern={preview ? undefined : "[A-Za-z_][A-Za-z0-9_]*"}
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
              required={!preview && valueLabel === "Secret key"}
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
