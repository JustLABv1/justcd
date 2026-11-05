/**
 * Tells the user which workspace they are publishing in. The workspace follows
 * the one selected in the sidebar, so publishing never asks for it again.
 */
export function WorkspaceNote({ name }: { name?: string }) {
  if (!name) return null
  return (
    <p className="text-sm leading-6 text-muted-foreground">
      Publishing in workspace{" "}
      <span className="font-medium text-foreground">{name}</span>. Switch it
      with the workspace menu in the sidebar.
    </p>
  )
}
