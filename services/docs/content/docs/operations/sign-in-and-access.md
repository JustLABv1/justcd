---
title: "Sign-in, registration and workspace creation"
---

Instance administrators control how people get into JustCD under **Instance settings → Sign-in & access**. Every change is written to the audit trail as `instance.access_updated` with what changed.

## Sign-in methods

**Password sign-in** is on by default. Turn it off to make single sign-on (OIDC) the only way in; the login page then shows only your providers and the password endpoint answers `403`.

To avoid locking everyone out, JustCD only lets you turn password sign-in off when at least one OIDC provider is enabled **and** an active administrator has already signed in through one. Until then the switch is disabled and the page says what is missing.

### Recovery

If your identity provider is unavailable after password sign-in was turned off, start the backend with `JUSTCD_FORCE_LOCAL_LOGIN=true` (Helm: `forceLocalLogin: true`). Password sign-in is then available again regardless of the setting, and the settings page shows a warning while the override is active. Remove it once single sign-on works.

## Registration

There is no public sign-up. The first administrator is created during setup, and local accounts are created by administrators under **Platform users**.

The one way accounts appear on their own is **single sign-on**: by default, anyone who signs in through an enabled provider with a verified email address gets an account (workspace access still depends on group mapping or an invitation). You can:

- turn **Create accounts on first single sign-on sign-in** off, so only people an administrator already added can sign in. A new identity is refused with a message to contact an administrator;
- keep it on but limit it to **allowed email domains**. An empty list allows any domain.

These rules apply only to people who do not have an account yet. Accounts that already exist, including ones an administrator created, always keep working.

## Workspace creation

By default every signed-in user can create a workspace and becomes its owner. Choose **Instance administrators only** to reserve that for administrators. The "New workspace" actions disappear for everyone else and the API refuses them with `403`.
