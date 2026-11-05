"use client"

import { FeatureCard } from "@/components/feature-card"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { FormField, SwitchField } from "@/components/ui-kit"
import { packageEcosystems, type PackageSource } from "@/lib/publishing"

function parse(value: string): PackageSource[] {
  try {
    const parsed = value ? JSON.parse(value) : []
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

/**
 * Routes package managers through the platform's mirror, for example JFrog
 * Artifactory remote repositories: where to install from, whether to sign in,
 * and whether uploads may reach for any other source.
 */
export function PackageSourcesCard({
  sources: raw,
  policy,
  onChange,
  secretMode,
  user,
  token,
  onCredentials,
  missing,
  storedCredentials,
  credentialsNote,
}: {
  sources: string
  policy: string
  onChange: (next: { packageSources: string; packagePolicy: string }) => void
  secretMode: string
  user: string
  token: string
  onCredentials: (next: { packageUser?: string; packageToken?: string }) => void
  /** Set when the credentials still have to be entered. */
  missing?: boolean
  /** Credentials exist already and are kept when the fields stay empty. */
  storedCredentials?: boolean
  /** Overrides where the credentials end up, for example in an app's GitLab project. */
  credentialsNote?: string
}) {
  const sources = parse(raw)
  const set = (next: PackageSource[], nextPolicy = policy) =>
    onChange({
      packageSources: next.length ? JSON.stringify(next) : "",
      packagePolicy: next.length ? nextPolicy : "",
    })
  const used = new Set(sources.map((s) => s.ecosystem))
  const free = packageEcosystems.find((e) => !used.has(e.value))
  const authenticated = sources.some((s) => s.authenticated)
  const patch = (index: number, changes: Partial<PackageSource>) =>
    set(sources.map((s, i) => (i === index ? { ...s, ...changes } : s)))
  const names = sources
    .map(
      (s) =>
        packageEcosystems
          .find((e) => e.value === s.ecosystem)
          ?.label.split(" (")[0]
    )
    .join(", ")
  return (
    <FeatureCard
      id="profile-card-packages"
      title="Package sources"
      description="Install Python, R, Node and other packages through your own mirror, such as JFrog Artifactory, with sign-in and your CA."
      summary={
        sources.length
          ? `${names}${policy === "enforce" ? " · only these sources" : ""}`
          : "Packages come from the base image or the public internet"
      }
      attention={missing ? "Required" : undefined}
      configured={sources.length > 0}
      sheetTitle="Package sources"
      sheetDescription={
        credentialsNote ??
        "Builds use these mirrors for each package manager. Credentials stay encrypted and never enter Git, the image or the app."
      }
    >
      {sources.map((source, index) => {
        const ecosystem = packageEcosystems.find(
          (e) => e.value === source.ecosystem
        )
        return (
          <div key={index} className="space-y-3 rounded-xl border p-4">
            <div className="grid gap-3 sm:grid-cols-[14rem_1fr]">
              <FormField
                label="Package manager"
                htmlFor={`package-ecosystem-${index}`}
              >
                <FormSelect
                  id={`package-ecosystem-${index}`}
                  value={source.ecosystem}
                  items={packageEcosystems
                    .filter(
                      (e) => !used.has(e.value) || e.value === source.ecosystem
                    )
                    .map((e) => ({ value: e.value, label: e.label }))}
                  onValueChange={(next) =>
                    patch(index, {
                      ecosystem: next as PackageSource["ecosystem"],
                    })
                  }
                />
              </FormField>
              <FormField
                label="Mirror URL"
                htmlFor={`package-url-${index}`}
                hint="Copy it from Artifactory's Set Me Up. https only, without credentials."
              >
                <Input
                  id={`package-url-${index}`}
                  value={source.url}
                  placeholder={ecosystem?.placeholder}
                  onChange={(event) =>
                    patch(index, { url: event.target.value })
                  }
                />
              </FormField>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="min-w-0 flex-1">
                <SwitchField
                  id={`package-auth-${index}`}
                  label="Requires sign-in"
                  checked={!!source.authenticated}
                  onCheckedChange={(next) =>
                    patch(index, { authenticated: next })
                  }
                />
              </div>
              <Button
                type="button"
                variant="destructive"
                onClick={() => set(sources.filter((_, i) => i !== index))}
              >
                Remove
              </Button>
            </div>
          </div>
        )
      })}
      {!sources.length && (
        <p className="text-sm leading-6 text-muted-foreground">
          No mirror configured. Builds install packages from whatever the base
          image allows. Add one mirror per package manager your apps use.
        </p>
      )}
      <Button
        type="button"
        variant="outline"
        disabled={!free || sources.length >= 8}
        onClick={() =>
          free &&
          set([
            ...sources,
            { ecosystem: free.value, url: "", authenticated: true },
          ])
        }
      >
        Add package source
      </Button>
      {authenticated &&
        (secretMode === "vault" ? (
          <p className="rounded-xl border p-4 text-sm leading-6 text-muted-foreground">
            Package credentials come from the profile&apos;s Vault secret. Add
            the fields{" "}
            <code className="font-mono text-xs">package_username</code> and{" "}
            <code className="font-mono text-xs">package_token</code> next to the
            registry fields.
          </p>
        ) : (
          <div className="space-y-3 rounded-xl border p-4">
            <p className="text-sm font-medium">Package credentials</p>
            <p className="text-xs leading-5 text-muted-foreground">
              Use a read-only account. Installing packages runs project code, so
              the token should only be able to download.
              {storedCredentials
                ? " Leave empty to keep the stored values."
                : ""}
            </p>
            <div className="grid gap-3 sm:grid-cols-2">
              <FormField label="Username" htmlFor="package-user">
                <Input
                  id="package-user"
                  autoComplete="off"
                  value={user}
                  onChange={(event) =>
                    onCredentials({ packageUser: event.target.value })
                  }
                />
              </FormField>
              <FormField label="Token or API key" htmlFor="package-token">
                <Input
                  id="package-token"
                  type="password"
                  autoComplete="off"
                  minLength={8}
                  value={token}
                  onChange={(event) =>
                    onCredentials({ packageToken: event.target.value })
                  }
                />
              </FormField>
            </div>
          </div>
        ))}
      {sources.length > 0 && (
        <FormField
          label="Other package sources"
          htmlFor="package-policy"
          hint="With “only these sources”, uploads that name another index, a Git dependency or a foreign host are rejected with the file and reason."
        >
          <FormSelect
            id="package-policy"
            value={policy || "allow"}
            items={[
              { value: "allow", label: "Allow (mirrors are the default)" },
              { value: "enforce", label: "Only the sources above" },
            ]}
            onValueChange={(next) =>
              set(sources, next === "enforce" ? "enforce" : "")
            }
          />
        </FormField>
      )}
      {sources.length > 0 && (
        <p className="text-xs leading-5 text-muted-foreground">
          A mirror with a private certificate authority also needs the “Custom
          CA variable” under CI runner and trust. The same CA is used for
          package downloads.
        </p>
      )}
    </FeatureCard>
  )
}
