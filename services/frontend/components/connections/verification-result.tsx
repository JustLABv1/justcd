"use client"

import { Alert } from "@/components/panelui/alert"
import { Button } from "@/components/panelui/button"
import type { KubernetesPermissionReport } from "@/lib/types"

export function ConnectionVerificationResult({ report, busy, onRetry, onFinish }: { report: KubernetesPermissionReport; busy: boolean; onRetry: () => void; onFinish: () => void }) {
  const passed = report.status === "passed"
  return <div className="space-y-5">
    <Alert variant="plain" tone={passed ? "success" : report.status === "failed" ? "destructive" : "warning"}>
      <Alert.Icon />
      <Alert.Title>{passed ? "Passed" : "Needs attention"}</Alert.Title>
      <Alert.Description>{passed ? `All required namespace checks passed on ${report.serverVersion ?? "the Kubernetes API"}.` : report.failure?.message ?? `${report.checks.filter(item => item.status === "missing").length} required permissions are missing.`}</Alert.Description>
      {report.failure?.remediation && <Alert.Details label="How to fix" defaultOpen><Alert.Item>{report.failure.remediation}</Alert.Item></Alert.Details>}
    </Alert>
    <dl className="grid grid-cols-1 gap-4 sm:grid-cols-3">
      {[["Checks passed", `${report.checks.filter(item => item.status === "passed").length} / ${report.checks.length}`], ["Namespace", report.namespace], ["Read Pods", report.canReadPods ? "Allowed" : "Not allowed"]].map(([label,value]) => <div key={label} className="min-w-0"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-1 break-words text-sm font-medium">{value}</dd></div>)}
    </dl>
    <div className="flex flex-wrap justify-end gap-2 pt-3"><Button variant="outline" onClick={onRetry} loading={busy}>Run again</Button><Button onClick={onFinish}>{passed ? "Finish connection" : "Finish with warnings"}</Button></div>
  </div>
}
