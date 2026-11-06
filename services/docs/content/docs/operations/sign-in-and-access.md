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

There are three ways an account comes to exist.

**An administrator creates it** under **Platform users**. This is always available, and these accounts are trusted immediately.

**The person registers with a password** (off by default). Turn on **Let people register with a password** and the sign-in page offers *Create an account*. It requires password sign-in to be on, and turning password sign-in off closes registration with it. Because JustCD does not verify email addresses, a registered account starts **awaiting approval**:

- it can sign in, but cannot create workspaces, and workspace owners cannot add it;
- an administrator approves it with **Approve account** in Platform users;
- if the real owner of that address signs in through single sign-on first, the account is handed to them and the password, sessions and two-factor setup the registrant created are removed (audited as `user.registration_reclaimed`).

Registration is rate limited per address and per IP, enforces the allowed email domains below, and is audited as `user.registered`; approvals as `user.approved`.

**Single sign-on creates it** on first sign-in. By default, anyone who signs in through an enabled provider with a verified email address gets an account (workspace access still depends on group mapping or an invitation). You can turn **Create accounts on first single sign-on sign-in** off so only people an administrator already added can sign in.

**Allowed email domains** limit registration and first single sign-on sign-in to those domains; an empty list allows any. These rules only apply to people without an account. Accounts that already exist always keep working.

## Workspace creation

By default every signed-in user can create a workspace and becomes its owner. Choose **Instance administrators only** to reserve that for administrators. The "New workspace" actions disappear for everyone else and the API refuses them with `403`.
