"use client"

import { useState } from "react"
import { Disclosure } from "@/components/ui/collapsible"
import { FeatureCard } from "@/components/feature-card"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { FormSelect } from "@/components/form-select"
import { FormField, CheckboxField } from "@/components/ui-kit"
import type {
  DatabasePlan,
  ServiceCatalog,
  VaultSecretBinding,
} from "@/lib/publishing"
import { SettingsPanel } from "@/components/settings/settings-section"

const defaultDatabase: DatabasePlan = {
  name: "PostgreSQL",
  postgresVersion: "17",
  instances: 1,
  storage: "10Gi",
  databaseName: "app",
  username: "app",
  credentialMode: "generated",
  allowConfigure: true,
  allowEnvMapping: true,
  allowCustomPassword: true,
  credentialsSecret: "{app}-database",
  usernameKey: "username",
  passwordKey: "password",
  bootstrapSecret: "{app}-database",
  bootstrapKey: "init.sql",
}
export function PublishingServicesEditor({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const [openBinding, setOpenBinding] = useState<number | null>(null)
  let catalog: ServiceCatalog = {}
  try {
    catalog = (value ? JSON.parse(value) : {}) || {}
  } catch {
    /* Server validates saved catalog. */
  }
  const patch = (next: ServiceCatalog) =>
    onChange(
      next.database || next.vaultSecrets?.length || next.allowEnvironment
        ? JSON.stringify(next)
        : ""
    )
  const db = catalog.database
  const bindings = (catalog.vaultSecrets ?? []).map((binding) => ({
    ...binding,
    envs: binding.envs ?? [],
  }))
  const patchBinding = (index: number, changes: Partial<VaultSecretBinding>) =>
    patch({
      ...catalog,
      vaultSecrets: bindings.map((item, i) =>
        i === index ? { ...item, ...changes } : item
      ),
    })
  const field = (
    label: string,
    id: string,
    current: string,
    change: (v: string) => void,
    hint?: string
  ) => (
    <FormField label={label} htmlFor={id} hint={hint}>
      <Input
        id={id}
        value={current}
        onChange={(event) => change(event.target.value)}
      />
    </FormField>
  )
  const hint = (text: string) => (
    <p className="text-xs leading-5 text-muted-foreground">{text}</p>
  )
  return (
    <div className="space-y-5">
      <SettingsPanel surface="flat"
        title="Optional application services"
        description="Offer a StackGres database and approved Vault secrets. Turn a service on, then select its card to configure it. Secret values never enter Git."
      >
        <div className="grid gap-3">
          <FeatureCard
            id="services-allow-environment"
            title="App-specific environment variables"
            description="Users may add non-secret configuration values. Secret values must use approved Vault bindings."
            enabled={!!catalog.allowEnvironment}
            onEnabledChange={(allowEnvironment) =>
              patch({ ...catalog, allowEnvironment })
            }
          />
          <FeatureCard
            id="database-enabled"
            title="PostgreSQL database"
            description="Requires StackGres in the target cluster. Database data is retained during app deletion."
            enabled={!!db}
            onEnabledChange={(enabled) =>
              patch({
                ...catalog,
                database: enabled ? { ...defaultDatabase } : undefined,
              })
            }
            summary={
              db
                ? `${db.credentialMode === "generated" ? "Managed by JustCD" : "Platform credentials"} · PostgreSQL ${db.postgresVersion} · ${db.instances} instance${db.instances === 1 ? "" : "s"} · ${db.storage}`
                : undefined
            }
            sheetTitle="PostgreSQL database"
            sheetDescription="How the database is provisioned and what users may change."
          >
            {db && (
              <>
                <FormField
                  label="Database credentials"
                  htmlFor="database-credential-mode"
                  hint="JustCD-managed credentials allow users to choose their database name and username. Existing platform credentials keep those names fixed."
                >
                  <FormSelect
                    id="database-credential-mode"
                    value={db.credentialMode || "platform"}
                    items={[
                      {
                        value: "generated",
                        label: "Generate and provision securely with JustCD",
                      },
                      {
                        value: "platform",
                        label: "Existing platform or Vault Secrets",
                      },
                    ]}
                    onValueChange={(credentialMode) =>
                      patch({
                        ...catalog,
                        database: {
                          ...db,
                          credentialMode: credentialMode as
                            "generated" | "platform",
                          allowConfigure: credentialMode === "generated",
                          allowCustomPassword: credentialMode === "generated",
                          ...(credentialMode === "generated"
                            ? {
                                requiredVaultSecret: "",
                                bootstrapSecret: db.credentialsSecret,
                              }
                            : {}),
                        },
                      })
                    }
                  />
                </FormField>
                <CheckboxField
                  id="database-allow-configure"
                  label="Allow users to choose database name and username"
                  checked={!!db.allowConfigure}
                  disabled={db.credentialMode !== "generated"}
                  onCheckedChange={(allowConfigure) =>
                    patch({ ...catalog, database: { ...db, allowConfigure } })
                  }
                />
                <CheckboxField
                  id="database-allow-password"
                  label="Allow a user-supplied password"
                  checked={!!db.allowCustomPassword}
                  disabled={db.credentialMode !== "generated"}
                  onCheckedChange={(allowCustomPassword) =>
                    patch({
                      ...catalog,
                      database: { ...db, allowCustomPassword },
                    })
                  }
                  description="Otherwise JustCD always generates a password. Values are encrypted and excluded from Git and drafts."
                />
                <CheckboxField
                  id="database-allow-envs"
                  label="Allow custom database environment names"
                  checked={!!db.allowEnvMapping}
                  onCheckedChange={(allowEnvMapping) =>
                    patch({ ...catalog, database: { ...db, allowEnvMapping } })
                  }
                />
                <div className="grid gap-4 sm:grid-cols-2">
                  {field("Plan name", "database-plan-name", db.name, (name) =>
                    patch({ ...catalog, database: { ...db, name } })
                  )}
                  {field(
                    "Database name",
                    "database-name",
                    db.databaseName,
                    (databaseName) =>
                      patch({ ...catalog, database: { ...db, databaseName } }),
                    "Must match the database created by the bootstrap SQL."
                  )}
                  {field(
                    "Default app username",
                    "database-default-user",
                    db.username ?? "app",
                    (username) =>
                      patch({ ...catalog, database: { ...db, username } })
                  )}
                  {field(
                    "PostgreSQL version",
                    "database-version",
                    db.postgresVersion,
                    (postgresVersion) =>
                      patch({
                        ...catalog,
                        database: { ...db, postgresVersion },
                      })
                  )}
                </div>
                <Disclosure summary="Size, storage and backup">
                  <div className="grid gap-4 sm:grid-cols-2">
                    <FormField label="Instances" htmlFor="database-instances">
                      <FormSelect
                        id="database-instances"
                        value={String(db.instances)}
                        onValueChange={(v) =>
                          patch({
                            ...catalog,
                            database: { ...db, instances: Number(v) },
                          })
                        }
                        items={[1, 2, 3, 4, 5].map((v) => ({
                          value: String(v),
                          label: String(v),
                        }))}
                      />
                    </FormField>
                    {field(
                      "Storage size",
                      "database-storage",
                      db.storage,
                      (storage) =>
                        patch({ ...catalog, database: { ...db, storage } }),
                      "For example 10Gi. Allocated per instance."
                    )}
                    {field(
                      "Instance profile",
                      "database-instance-profile",
                      db.instanceProfile ?? "",
                      (instanceProfile) =>
                        patch({
                          ...catalog,
                          database: { ...db, instanceProfile },
                        }),
                      "Existing SGInstanceProfile. Empty uses the operator default."
                    )}
                    {field(
                      "Storage class",
                      "database-storage-class",
                      db.storageClass ?? "",
                      (storageClass) =>
                        patch({ ...catalog, database: { ...db, storageClass } })
                    )}
                    {field(
                      "Backup storage",
                      "database-backup",
                      db.backupStorage ?? "",
                      (backupStorage) =>
                        patch({
                          ...catalog,
                          database: { ...db, backupStorage },
                        }),
                      "Existing SGObjectStorage in the app namespace. Empty means no backup."
                    )}
                  </div>
                </Disclosure>
                <Disclosure summary="Secret names and bootstrap">
                  <div className="grid gap-4 sm:grid-cols-2">
                    {field(
                      "App credentials Secret",
                      "database-secret",
                      db.credentialsSecret,
                      (credentialsSecret) =>
                        patch({
                          ...catalog,
                          database: { ...db, credentialsSecret },
                        }),
                      db.credentialMode === "generated"
                        ? "Created by JustCD during deployment. Must contain {app}."
                        : "Existing or Vault-synchronized Secret. {app} and {namespace} are expanded. Not the StackGres superuser Secret."
                    )}
                    {field(
                      "Username key",
                      "database-username",
                      db.usernameKey,
                      (usernameKey) =>
                        patch({ ...catalog, database: { ...db, usernameKey } })
                    )}
                    {field(
                      "Password key",
                      "database-password-key",
                      db.passwordKey,
                      (passwordKey) =>
                        patch({ ...catalog, database: { ...db, passwordKey } })
                    )}
                    {field(
                      "Bootstrap SQL Secret",
                      "database-bootstrap",
                      db.bootstrapSecret,
                      (bootstrapSecret) =>
                        patch({
                          ...catalog,
                          database: { ...db, bootstrapSecret },
                        }),
                      db.credentialMode === "generated"
                        ? "Must equal the app credentials Secret."
                        : "Secret with SQL that creates the restricted role and database. Never paste SQL passwords here."
                    )}
                    {field(
                      "Bootstrap SQL key",
                      "database-bootstrap-key",
                      db.bootstrapKey,
                      (bootstrapKey) =>
                        patch({ ...catalog, database: { ...db, bootstrapKey } })
                    )}
                    <FormField
                      label="Required Vault binding"
                      htmlFor="database-vault-binding"
                      hint="Included automatically when the user chooses a database."
                    >
                      <FormSelect
                        disabled={db.credentialMode === "generated"}
                        id="database-vault-binding"
                        value={db.requiredVaultSecret ?? ""}
                        onValueChange={(requiredVaultSecret) =>
                          patch({
                            ...catalog,
                            database: { ...db, requiredVaultSecret },
                          })
                        }
                        items={[
                          {
                            value: "",
                            label: "Secrets provided by the platform",
                          },
                          ...bindings.map((v) => ({
                            value: v.id,
                            label: v.name || v.id,
                          })),
                        ]}
                      />
                    </FormField>
                  </div>
                </Disclosure>
              </>
            )}
          </FeatureCard>
        </div>
      </SettingsPanel>
      <SettingsPanel surface="flat"
        title="Vault secrets"
        description="Approve Vault locations once; users switch them on by name. Requires Vault Secrets Operator and an authorized VaultAuth."
        action={
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={bindings.length >= 10}
            onClick={() => {
              const ids = new Set(bindings.map((v) => v.id))
              let n = 1
              while (ids.has(`secret-${n}`)) n++
              patch({
                ...catalog,
                vaultSecrets: [
                  ...bindings,
                  {
                    id: `secret-${n}`,
                    name: "Application secret",
                    authRef: "default",
                    mount: "secret",
                    path: "apps/{app}",
                    type: "kv-v2",
                    refreshAfter: "1m",
                    destination: `{app}-secret-${n}`,
                    envs: [],
                  },
                ],
              })
              setOpenBinding(bindings.length)
            }}
          >
            Add Vault secret
          </Button>
        }
      >
        <div className="grid gap-3">
          {bindings.length === 0 && (
            <p className="py-4 text-sm text-muted-foreground">
              No Vault secrets approved yet. Add one so users can synchronize a
              secret into their app.
            </p>
          )}
          {bindings.map((v, i) => (
            <FeatureCard
              key={i}
              id={`vault-${i}`}
              title={v.name || "Vault secret"}
              description={
                <span className="flex flex-wrap gap-1.5">
                  <Badge
                    size="sm"

                    variant={v.allowPathSuffix ? "info" : "secondary"}
                  >
                    {v.allowPathSuffix ? "Users choose path" : "Fixed path"}
                  </Badge>
                  <Badge
                    size="sm"

                    variant={v.allowEnvMapping ? "info" : "secondary"}
                  >
                    {v.allowEnvMapping
                      ? "Users map variables"
                      : "Fixed variables"}
                  </Badge>
                </span>
              }
              summary={`${v.mount}/${v.path} · ${v.envs.length} environment variable${v.envs.length === 1 ? "" : "s"}`}
              open={openBinding === i}
              onOpenChange={(open) => setOpenBinding(open ? i : null)}
              sheetTitle={v.name || "Vault secret"}
              sheetDescription="Approved Vault location and what users may change."
              sheetFooter={
                <Button
                  type="button"
                  variant="destructive"
                  onClick={() => {
                    setOpenBinding(null)
                    patch({
                      ...catalog,
                      vaultSecrets: bindings.filter((_, n) => n !== i),
                      database:
                        db?.requiredVaultSecret === v.id
                          ? { ...db, requiredVaultSecret: "" }
                          : db,
                    })
                  }}
                >
                  Remove
                </Button>
              }
            >
              <h3 className="text-sm font-medium">Location</h3>
              <div className="grid gap-4 sm:grid-cols-2">
                {field("Display name", `vault-${i}-name`, v.name, (name) =>
                  patchBinding(i, { name })
                )}
                {field(
                  "Binding ID",
                  `vault-${i}-id`,
                  v.id,
                  (id) => patchBinding(i, { id }),
                  "Lowercase, up to 30 characters. Keep it unchanged once apps use it."
                )}
                {field("KV mount", `vault-${i}-mount`, v.mount, (mount) =>
                  patchBinding(i, { mount })
                )}
                {field(
                  "Secret path",
                  `vault-${i}-path`,
                  v.path,
                  (path) => patchBinding(i, { path }),
                  "Relative to the mount, without /data/. Supports {app} and {namespace}."
                )}
              </div>
              <h3 className="text-sm font-medium">What users may change</h3>
              <CheckboxField
                id={`vault-${i}-allow-path`}
                label="Allow users to choose a secret below this path"
                checked={!!v.allowPathSuffix}
                onCheckedChange={(allowPathSuffix) =>
                  patchBinding(i, { allowPathSuffix })
                }
                description="The configured path remains the boundary. Users can only add a relative suffix."
              />
              <CheckboxField
                id={`vault-${i}-allow-env`}
                label="Allow users to map Secret keys to environment variables"
                checked={!!v.allowEnvMapping}
                onCheckedChange={(allowEnvMapping) =>
                  patchBinding(i, { allowEnvMapping })
                }
              />
              <Disclosure summary="Authentication and synchronization">
                <div className="grid gap-4 sm:grid-cols-2">
                  {field(
                    "VaultAuth reference",
                    `vault-${i}-auth`,
                    v.authRef,
                    (authRef) => patchBinding(i, { authRef }),
                    "Name or namespace/name of the preconfigured VaultAuth."
                  )}
                  {field(
                    "Vault namespace (optional)",
                    `vault-${i}-namespace`,
                    v.vaultNamespace ?? "",
                    (vaultNamespace) => patchBinding(i, { vaultNamespace })
                  )}
                  <FormField label="KV engine" htmlFor={`vault-${i}-type`}>
                    <FormSelect
                      id={`vault-${i}-type`}
                      value={v.type}
                      onValueChange={(type) => patchBinding(i, { type })}
                      items={[
                        { value: "kv-v2", label: "KV v2" },
                        { value: "kv-v1", label: "KV v1" },
                      ]}
                    />
                  </FormField>
                  {field(
                    "Refresh interval",
                    `vault-${i}-refresh`,
                    v.refreshAfter,
                    (refreshAfter) => patchBinding(i, { refreshAfter }),
                    "For example 1m or 1h. VSO restarts the app when data changes."
                  )}
                  {field(
                    "Destination Secret",
                    `vault-${i}-destination`,
                    v.destination,
                    (destination) => patchBinding(i, { destination }),
                    "Created by VSO in the app namespace. Supports {app} and {namespace}."
                  )}
                </div>
              </Disclosure>
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-sm font-medium">
                    Environment variable mappings
                  </p>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={v.envs.length >= 30}
                    onClick={() =>
                      patchBinding(i, {
                        envs: [...v.envs, { name: "", key: "" }],
                      })
                    }
                  >
                    Add mapping
                  </Button>
                </div>
                {v.envs.length === 0 &&
                  hint("No mappings: the Secret is created without variables.")}
                {v.envs.map((e, j) => (
                  <div
                    key={j}
                    className="grid items-end gap-3 sm:grid-cols-[1fr_1fr_auto]"
                  >
                    {field(
                      "Environment variable",
                      `vault-${i}-env-${j}`,
                      e.name,
                      (name) =>
                        patchBinding(i, {
                          envs: v.envs.map((item, n) =>
                            n === j ? { ...item, name } : item
                          ),
                        })
                    )}
                    {field("Secret key", `vault-${i}-key-${j}`, e.key, (key) =>
                      patchBinding(i, {
                        envs: v.envs.map((item, n) =>
                          n === j ? { ...item, key } : item
                        ),
                      })
                    )}
                    <Button
                      type="button"
                      variant="destructive"
                      onClick={() =>
                        patchBinding(i, {
                          envs: v.envs.filter((_, n) => n !== j),
                        })
                      }
                    >
                      Remove
                    </Button>
                  </div>
                ))}
              </div>
            </FeatureCard>
          ))}
        </div>
      </SettingsPanel>
    </div>
  )
}
