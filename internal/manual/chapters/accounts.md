# Accounts and Signing In

Everything in prova needs a signed-in account. The only things anyone can reach without one are the sign-in screen, this manual, and the health check a load balancer polls.

## Roles

Every account has one role, and each role includes the ones before it.

| Role | May |
|---|---|
| viewer | read every run, proof, message, suite, report and the manual |
| operator | also run scenarios, scripts, suites, load tests and transformations |
| admin | also manage accounts and read the activity log |

> Give people the least they need. A release manager who reviews proofs is a viewer; an engineer who runs suites is an operator; only the people who look after prova itself are administrators.

## Three ways in

Any combination can be enabled at once; the sign-in screen offers whichever are configured.

- **A local account**: a name and a password prova generated.
- **OpenID Connect**: Microsoft Entra ID, Okta, Google, Keycloak, Auth0 and any other conforming provider, using the authorization code flow with PKCE.
- **SAML 2.0**: Entra ID, Okta, ADFS, Keycloak and others.

Single sign-on proves **who** someone is. The account in prova says **what** they may do. A person the identity provider vouches for who has no prova account marked for single sign-on is refused — never created. That way an identity provider misconfigured to admit the whole company does not give the whole company access.

## Local accounts

On first start prova creates `admin` and prints its password once. After that, accounts are managed on the **Users** screen or from the command line:

```
prova user add alice operator      # prints alice's password once
prova user reset alice             # a new password; her sessions end
prova user sso dana viewer         # dana signs in only through OIDC or SAML
prova user remove alice
prova user list
```

Passwords are generated, never chosen: twenty characters from a 56-character alphabet, about 116 bits. A generated password is strong enough that no rule about what it must contain is needed, and it cannot be one somebody also uses elsewhere. It is shown once and stored only as a salted PBKDF2-SHA256 hash at 600,000 iterations.

Names are lower case letters, digits, `.`, `_` and `-`, starting with a letter. An identity provider's `Dana@Corp.example` becomes the account `dana`.

## OpenID Connect

Register prova with your provider as a web application whose redirect URI is `PUBLIC-URL/api/auth/oidc/callback`, then start prova with its public address and the provider:

```
prova -public-url https://prova.example.com:8443 \
      -oidc-issuer https://login.microsoftonline.com/TENANT/v2.0 \
      -oidc-client-id CLIENT-ID -oidc-client-secret-file /etc/prova/oidc-secret
```

The secret can also be given in the environment as `PROVA_OIDC_CLIENT_SECRET`; it is never accepted on the command line, where every user of the machine could read it. The account name is read from `preferred_username` unless `-oidc-name-claim` names another claim. `-oidc-group` requires membership of a group, read from the `groups` claim or the one `-oidc-groups-claim` names.

## SAML 2.0

Give the identity provider prova's service-provider metadata, at `PUBLIC-URL/api/auth/saml/metadata`, and give prova the provider's:

```
prova -public-url https://prova.example.com:8443 \
      -saml-idp-metadata https://login.microsoftonline.com/TENANT/federationmetadata/2007-06/federationmetadata.xml
```

The metadata may be an https URL or a file. A response is trusted only when its signature verifies against the certificate in the provider's metadata (never one carried in the response), its audience and destination are prova, its conditions are current, it answers a sign-in prova started, and it has not been seen before. The account name is taken from an email attribute, then a user principal name, then the NameID, unless `-saml-name-attribute` names one. `-saml-group` requires membership of a group.

## Sessions

A session is kept in prova's database, so restarting prova does not sign anyone out. It ends after 30 minutes without a request, after 12 hours regardless, when the person signs out, and at once when their account's role, password or sign-in method changes or the account is removed.

The session cookie is `HttpOnly`, `Secure` and `SameSite=Strict`. The database holds only the SHA-256 of each session token, so a copy of the database cannot be used to sign in as anybody.

Five wrong passwords for a name within 15 minutes lock that name for the rest of the 15 minutes. A wrong name and a wrong password get the same answer in the same time, so the sign-in screen cannot be used to find out which names exist.

## A public demonstration

`-demo-account demo:PASSWORD` creates a viewer account whose name and password the sign-in screen displays, for a public demonstration. It can look at every screen and change nothing, and it cannot be edited from the Users screen. Combine it with `-demo-traffic 15m` so the demonstration shows fresh verdicts, and `-retain-days 7` so its history does not grow without bound.
