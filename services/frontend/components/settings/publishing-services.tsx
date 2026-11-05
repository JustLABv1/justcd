"use client"

import { useState } from "react"
import { Disclosure } from "@/components/ui/collapsible"
import { PublishingServicesPicker } from "@/components/applications/publishing-services"
import type { ServiceSelection } from "@/lib/publishing"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { FormField, Panel, SwitchField } from "@/components/ui-kit"
import type {
  DatabasePlan,
  ServiceCatalog,
  VaultSecretBinding,
} from "@/lib/publishing"

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
  const [preview, setPreview] = useState<ServiceSelection>({
    database: false,
    vaultSecrets: [],
  })
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
  return (
    <Panel
      title="Optional application services"
      description="Offer a StackGres database and approved Vault secrets. Applications choose what they need; secret values never enter Git."
    >
      <div className="space-y-5 p-5">
        <SwitchField
          id="services-allow-environment"
          label="Allow app-specific environment variables"
          checked={!!catalog.allowEnvironment}
          onCheckedChange={(allowEnvironment) =>
            patch({ ...catalog, allowEnvironment })
          }
          description="Users may add non-secret configuration values. Secret values must use approved Vault bindings."
        />
        <SwitchField
          id="database-enabled"
          label="Offer a PostgreSQL database"
          checked={!!db}
          onCheckedChange={(enabled) =>
            patch({
              ...catalog,
              database: enabled ? { ...defaultDatabase } : undefined,
            })
          }
          description="Requires StackGres in the target cluster. Choose how credentials are provisioned below. Database data is retained during app deletion."
        />
        {db && (
          <div className="space-y-4 rounded-lg border p-4">
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
            <SwitchField
              id="database-allow-configure"
              label="Allow users to choose database name and username"
              checked={!!db.allowConfigure}
              disabled={db.credentialMode !== "generated"}
              onCheckedChange={(allowConfigure) =>
                patch({ ...catalog, database: { ...db, allowConfigure } })
              }
            />
            <SwitchField
              id="database-allow-password"
              label="Allow a user-supplied password"
              checked={!!db.allowCustomPassword}
              disabled={db.credentialMode !== "generated"}
              onCheckedChange={(allowCustomPassword) =>
                patch({ ...catalog, database: { ...db, allowCustomPassword } })
              }
              description="Otherwise JustCD always generates a password. Values are encrypted and excluded from Git and drafts."
            />
            <SwitchField
              id="database-allow-envs"
              label="Allow custom database environment names"
              checked={!!db.allowEnvMapping}
              onCheckedChange={(allowEnvMapping) =>
                patch({ ...catalog, database: { ...db, allowEnvMapping } })
              }
            />
            <div className="grid gap-4 md:grid-cols-2">
              {field(
                "Default app username",
                "database-default-user",
                db.username ?? "app",
                (username) =>
                  patch({ ...catalog, database: { ...db, username } })
              )}
              {field("Plan name", "database-plan-name", db.name, (name) =>
                patch({ ...catalog, database: { ...db, name } })
              )}
              {field(
                "PostgreSQL version",
                "database-version",
                db.postgresVersion,
                (postgresVersion) =>
                  patch({ ...catalog, database: { ...db, postgresVersion } })
              )}
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
                "For example 10Gi. Storage is allocated per database instance."
              )}
              {field(
                "Instance profile",
                "database-instance-profile",
                db.instanceProfile ?? "",
                (instanceProfile) =>
                  patch({ ...catalog, database: { ...db, instanceProfile } }),
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
                  patch({ ...catalog, database: { ...db, backupStorage } }),
                "Existing SGObjectStorage in the app namespace. Empty means no backup configuration is added."
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
                "App credentials Secret",
                "database-secret",
                db.credentialsSecret,
                (credentialsSecret) =>
                  patch({ ...catalog, database: { ...db, credentialsSecret } }),
                db.credentialMode === "generated"
                  ? "Dedicated Secret created by JustCD during deployment. Must contain {app}."
                  : "Existing or Vault-synchronized Secret. {app} and {namespace} are expanded. Do not use the StackGres superuser Secret."
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
                  patch({ ...catalog, database: { ...db, bootstrapSecret } }),
                db.credentialMode === "generated"
                  ? "Must equal the app credentials Secret. JustCD supplies bootstrap SQL securely."
                  : "Secret containing SQL to create the restricted role and database. Supplied by your platform or a Vault binding; never paste SQL passwords here."
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
                hint="Automatically included when the user chooses a database. Leave empty if namespace provisioning supplies the Secrets."
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
                    { value: "", label: "Secrets provided by the platform" },
                    ...bindings.map((v) => ({
                      value: v.id,
                      label: v.name || v.id,
                    })),
                  ]}
                />
              </FormField>
            </div>
          </div>
        )}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-medium">Vault static secrets</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Requires Vault Secrets Operator and an existing authorized
              VaultAuth. Approve paths here; users select bindings by name.
            </p>
          </div>
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
            }}
          >
            Add Vault secret
          </Button>
        </div>
        {bindings.map((v, i) => (
          <div key={i} className="space-y-4 rounded-lg border p-4">
            <div className="flex items-center justify-between gap-3">
              <h4 className="text-sm font-medium">
                {v.name || "Vault secret"}
              </h4>
              <Button
                type="button"
                size="sm"
                variant="destructive"
                onClick={() =>
                  patch({
                    ...catalog,
                    vaultSecrets: bindings.filter((_, n) => n !== i),
                    database:
                      db?.requiredVaultSecret === v.id
                        ? { ...db, requiredVaultSecret: "" }
                        : db,
                  })
                }
              >
                Remove
              </Button>
            </div>
            <SwitchField
              id={`vault-${i}-allow-path`}
              label="Allow users to choose a secret below this path"
              checked={!!v.allowPathSuffix}
              onCheckedChange={(allowPathSuffix) =>
                patchBinding(i, { allowPathSuffix })
              }
              description="The configured path remains the boundary. Users can only add a relative suffix."
            />
            <SwitchField
              id={`vault-${i}-allow-env`}
              label="Allow users to map Secret keys to environment variables"
              checked={!!v.allowEnvMapping}
              onCheckedChange={(allowEnvMapping) =>
                patchBinding(i, { allowEnvMapping })
              }
            />
            <div className="grid gap-4 md:grid-cols-2">
              {field(
                "Binding ID",
                `vault-${i}-id`,
                v.id,
                (id) => patchBinding(i, { id }),
                "Stable lowercase identifier, up to 30 characters. Keep it unchanged once apps use it."
              )}
              {field("Display name", `vault-${i}-name`, v.name, (name) =>
                patchBinding(i, { name })
              )}
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
                "For example 1m or 1h. VSO restarts the app when synchronized data changes."
              )}
              {field(
                "Destination Secret",
                `vault-${i}-destination`,
                v.destination,
                (destination) => patchBinding(i, { destination }),
                "Created by VSO in the app namespace. Supports {app} and {namespace}."
              )}
            </div>
            <div className="space-y-3">
              <div className="flex items-center justify-between gap-3">
                <p className="text-xs font-medium">
                  Environment variable mappings
                </p>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
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
                    size="sm"
                    variant="destructive"
                    onClick={() =>
                      patchBinding(i, {
                        envs: v.envs.filter((_, n) => n !== j),
                      })
                    }
                  >
                    Remove mapping
                  </Button>
                </div>
              ))}
            </div>
          </div>
        ))}
        <Disclosure summary="Preview publishing fields">
          <p className="mb-4 text-xs text-muted-foreground">
            Try the choices a user will see. Preview values do not change
            profile defaults and are not saved.
          </p>
          <PublishingServicesPicker
            preview
            options={{
              databaseName: db?.name,
              database: db,
              databaseVaultSecret: db?.requiredVaultSecret,
              allowEnvironment: catalog.allowEnvironment,
              vaultSecrets: bindings,
            }}
            value={preview}
            onChange={setPreview}
          />
        </Disclosure>
      </div>
    </Panel>
  )
}
