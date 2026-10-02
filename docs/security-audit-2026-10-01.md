# Security Audit — 2026-10-01

Geprüfter Stand: `1cb5b7e`. Schwerpunkt: Cluster-Agent, Enrollment, Token-Rotation,
Widerruf, Task-Ausführung und Helm-RBAC. Ergänzend: Workspace-Autorisierung,
Login/CSRF, Kubernetes-Zugangsdaten, Git/Rendering und Secret-Verschlüsselung.
Quellcodeprüfung und isolierte lokale Datenbanktests; kein Live-Cluster-Pentest.
Der ursprüngliche Audit änderte keinen Produktionscode. Die unten dokumentierten
Korrekturen wurden anschließend auf Wunsch des Nutzers umgesetzt.

## Befunde

### A1 — Hoch: Breite Standard-RBAC und indirekte Eskalation durch Workloads

Stellen: `charts/justcd-agent/values.yaml:25`,
`charts/justcd-agent/templates/rbac.yaml`,
`services/backend/internal/agentprotocol/protocol.go:66`.

Die Standardregeln erlauben Lesen und Mutieren sämtlicher Ressourcen sämtlicher
API-Gruppen. Standardmäßig gelten diese Regeln in den konfigurierten Namespaces;
mit `rbac.clusterWide: true` clusterweit. Die lokale Agent-Prüfung begrenzt
Request-Pfade und Namespaces, prüft aber keine Pod-/Workload-Sicherheitsrichtlinien.

Wer über einen autorisierten Deployment-Pfad Pod-Spezifikationen kontrolliert,
kann andere ServiceAccounts im zugelassenen Namespace verwenden oder dortige
Secrets als Volumes mounten. Ohne passende Admission-Regeln können privilegierte
Pods, Host-Namespace-Zugriff und hostPath-Mounts bis zur Node-Kompromittierung führen.
Ein Cluster-Admin-Token in einem Secret vergrößert entsprechend die Auswirkungen.
Der SecurityContext des Agent-Pods schützt nicht automatisch die von ihm erstellten Pods.

Dies ist ein konfigurationsabhängiges Berechtigungsrisiko, kein nachgewiesener
unauthentifizierter Agent-Exploit. Kubernetes-RBAC-Eskalationsschutz gilt weiterhin;
Wildcard-Regeln sind nicht pauschal mit dem eingebauten `cluster-admin` gleichzusetzen.

Empfehlung: Explizite Ressourcenregeln als Standard, getrennte Credentials für
Profile, dedizierter Agent-Namespace außerhalb der Deployment-Allowlist sowie
Pod Security Admission/admission policies für verwaltete Namespaces. Clusterweite
Rechte gezielt und dokumentiert vergeben.

Referenz: [Kubernetes RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/).

### A2 — Mittel: Alter Agent-Token kann seine Übergangsfrist verlängern

Stelle: `services/backend/internal/store/cluster_agents.go:151`.

`RenewClusterAgent` akzeptiert den vorherigen Token innerhalb der fünfminütigen
Übergangsfrist. Auch bei dieser Variante setzt das UPDATE dessen Ablauf wieder
auf NOW()+5 Minuten, ohne seinen Hash zu ersetzen. Wiederholte Requests können
die Gültigkeit dieses alten Tokens unbegrenzt verlängern. Zusätzlich wird dabei
jeweils der aktuelle Token ersetzt; ein konkurrierender Agent kann seine Identität verlieren.

Voraussetzung ist Besitz eines bereits gültigen vorherigen Tokens. Ein gestohlener
aktueller Token ist ohnehin zur Rotation berechtigt; dieser Befund betrifft
speziell die nicht eingehaltene zeitliche Grenze der alten Identität.

Reproduziert in einem isolierten PostgreSQL-Schema: Restlaufzeit des vorherigen
Tokens auf eine Sekunde gesetzt, damit erneut rotiert, danach Restlaufzeit 300 Sekunden.

Empfehlung: Beim vorherigen Token die ursprüngliche Ablaufzeit bewahren; eine
begrenzte, idempotente Wiederaufnahme der Rotation statt weiterer Rotationen vorsehen.

### A3 — Mittel: Widerruf und Task-Abholung sind nicht atomar

Stellen: `services/backend/internal/api/cluster_agents.go:166`,
`services/backend/internal/store/cluster_agents.go:114`.

Der HTTP-Handler authentifiziert und ruft danach getrennt `ClaimAgentTask` auf.
Die Claim-Abfrage prüft den Agent-Zustand nicht. Erfolgt der Widerruf zwischen
beiden Aktionen, kann der vorher gültige Poll dennoch einen wartenden Auftrag
einschließlich seines entschlüsselten Inhalts erhalten. Bereits laufende Arbeiten
zu beenden ist davon zu unterscheiden; hier geht es um neu abgeholte Arbeit.

Reproduziert auf Store-Ebene: Task eingestellt, Agent widerrufen, anschließend
Task erfolgreich geclaimt. Das bestätigt die fehlende Sperre; ein HTTP-Race wurde
nicht zeitlich erzwungen. Reguläre Polls nach abgeschlossenem Widerruf werden abgelehnt.

Empfehlung: Authentifizierung/Token-Prüfung, Widerruf und Claim über denselben
Agent-Datensatz serialisieren. Die Claim-Transaktion muss die Identität und den
Widerruf prüfen und dessen Lock bis zum Claim halten.

### A4 — Mittel: Login-Limit kann alle Nutzer hinter dem Frontend-Proxy sperren

Stellen: `services/backend/internal/api/auth.go:68` und `:97`,
`services/frontend/app/api/[...path]/route.ts`.

Das Limit ist acht Versuche je RemoteAddr-IP in 15 Minuten. Die UI leitet Requests
über den Next.js-Server weiter; damit sieht das Backend dessen IP für sämtliche
Nutzer. Acht erfolglose Versuche können deshalb alle Passwort-Logins über denselben
Proxy blockieren. Der Effekt wurde aus dem Request-Pfad abgeleitet, nicht gegen
aktive Benutzer getestet. Die genaue Reichweite hängt von Proxy-Instanzen ab.

Empfehlung: Client-IP über eine ausdrücklich konfigurierte Vertrauenskette
weitergeben und zusätzlich Account-/globale Limits einsetzen. Beliebige eingehende
X-Forwarded-For-Header dürfen nicht ungeprüft vertraut werden.

### A5 — Härtung: Keine Begrenzung paralleler Agent-Polls

Stelle: `services/backend/internal/api/cluster_agents.go:166`.

Ein gültiger Agent-Token kann beliebig viele parallele 20-Sekunden-Polls öffnen.
Jeder Poll authentifiziert und fragt alle 250 ms erneut nach Arbeit: ungefähr
160 Datenbankabfragen pro leerem Poll über 20 Sekunden. Das Task-Queue-Limit
begrenzt diese Last nicht. Eine Last-/Erschöpfungsgrenze wurde nicht gemessen.

Empfehlung: Begrenzte parallele Polls je Cluster/Identität, Rate-Limits und
ereignisgesteuerte oder weniger häufige Abholung. Enrollment separat limitieren.

## Positiv geprüfte Schutzmaßnahmen

- Enrollment-Token einmalig, zeitlich begrenzt und gehasht; atomare Verwendung
  und bestehende Bindung an die Cluster-UID.
- Agent-Verbindungen über HTTPS, keine Redirects, lokale Kubernetes-Credentials
  werden nicht als Profile an das Backend übertragen.
- Agent-Requests prüfen Workspace, Profil, Namespace, Cluster-Scope, Methode,
  Pfad und Deadline; gefährliche Kubernetes-Subresources werden eingeschränkt.
- Task-/Result-Payloads werden verschlüsselt, mit kontextgebundener AES-GCM-
  Authentifizierung; Resultate sind an Cluster, Task-ID und Lease gebunden.
- Workspace-Zugriff und Owner-Rechte für Agent-Konfiguration werden geprüft;
  Browser-Mutationen verwenden Session-Authentifizierung und CSRF-Schutz.
- Direkte Kubernetes-Kubeconfigs führen keine exec-/auth-provider-Plugins aus.
  Git nutzt Argumentlisten statt Shell-Interpolation; Rendering prüft Repository-Pfade.

Diese Ergebnisse sind geprüfte Kontrollen, keine Garantie für die Abwesenheit
weiterer Fehler. Das Backend bleibt ein vertrauenswürdiger Task-Aussteller;
der Agent verifiziert keine unabhängige menschliche Deployment-Freigabe.

## Verifikation und Grenzen

- Temporärer Integrationstest mit echten Migrationen in einem isolierten,
  anschließend entfernten PostgreSQL-Schema: A2 und Store-Verhalten von A3 bestätigt.
  Keine echten Tokens oder vorhandenen Cluster-Daten verwendet; Testdatei entfernt.
- `go test -race ./internal/agentprotocol ./internal/clusteragent ./internal/kube ./internal/security`.
- `govulncheck ./...`: keine erreichbare bekannte Schwachstelle gemeldet.
  Paketbefund [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) für grpc 1.83.1,
  behoben in 1.83.2, laut Scanner kein erreichbarer betroffener Aufruf.
  Modulbefund [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) für das
  unmaintained OpenPGP-Paket, ebenfalls kein erreichbarer betroffener Aufruf.
  grpc-Update als vorsorgliche Dependency-Pflege einplanen.
- Nicht enthalten: vollständiger Frontend-Abhängigkeitsscan, Image-/Container-Scan,
  tatsächliche Kubernetes-Admission-/Netzwerk-Konfiguration, Lasttest und vollständige
  Prüfung aller Deployment-/Approval-Workflow-Zustände.

Priorität: A1-Berechtigungen/Admission klären, A2 und A3 im Agent-Protokoll beheben,
dann A4 und A5. Agent-Pod-SecurityContext allein behebt A1 nicht.

## Umsetzung der Befunde

- **A1:** Chart 0.2 verwendet explizite Workload-RBAC. Eine standardmäßig aktive
  ValidatingAdmissionPolicy verlangt restricted/latest Namespace-Labels und
  ServiceAccount-/Secret-Allowlists. Lokal schützt `deniedNamespaces` die Agent-
  und System-Namespaces auch bei Cluster-Scope. Kubernetes >=1.30 ist erforderlich.
  Bestehende Namespace-Labels/Admission-Konfiguration und extern gemountete Tokens
  bleiben Betreiberverantwortung; siehe `docs/cluster-agents.md` für das Upgrade.
- **A2:** Rotation mit vorherigem Token bewahrt dessen ursprünglichen Ablauf.
- **A3:** Claim prüft Widerruf, Ablauf und den vom HTTP-Handler gelieferten Token
  unter einer Agent-Zeilensperre bis zum Commit. Nach Widerruf kein neuer Claim.
- **A4:** Passwort-Login-Limits sind pro normalisiertem Account statt Proxy-IP.
  Andere Accounts werden durch acht Fehlversuche gegen einen Account nicht gesperrt.
- **A5:** Ein aktiver Poll je Cluster und Backend-Prozess, maximal 120 Starts/Minute,
  einsekündiger DB-Pollabstand; Enrollment zehn Versuche/Minute je direkter Peer-IP.
  Mehrere Replikate benötigen ergänzende gemeinsame Ingress-Limits.
- grpc wurde vorsorglich auf 1.83.2 aktualisiert.

Regressionen prüfen unveränderte Token-Grace, abgelaufene/ersetzte/widerrufene Tokens,
Widerruf mit konkurrierendem Claim, Account-Trennung, Poll-Freigabe/Ratenbegrenzung,
geschützte Namespaces und die gerenderten CEL-Admission-Ausdrücke gegen sichere sowie
unerlaubte Pod-Spezifikationen. CEL-Tests ersetzen keinen API-Server-/Admission-Test
in der Zielumgebung.

Verifikation nach Umsetzung: vollständiges `go test ./...` erfolgreich; Race-Tests
für API, Store, Protokoll und Agent erfolgreich; Agent-/Activity-/Login-Integrationstests
in isoliertem PostgreSQL-Schema erfolgreich (einschließlich Race-Detector).
`go test -C charts/justcd-agent/tests -v ./...` und Helm-Lint erfolgreich; geprüft
wurden auch clusterweite RBAC, CEL-Namespace-Matching und die lokale Installations-
Namespace-Sperre. Erneuter govulncheck: null erreichbare Befunde, null Paketbefunde;
nur der nicht verwendete OpenPGP-Modulbefund bleibt. Kein Test oder Deployment
gegen den vorhandenen Kubernetes-Kontext wurde ausgeführt.
