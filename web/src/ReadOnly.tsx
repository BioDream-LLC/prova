import { Eye } from 'lucide-react'

/** ReadOnly stands where a Run button would be, for a viewer. */
export function ReadOnly({ what = 'run' }: { what?: string }) {
  return (
    <span
      className="inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg border border-line bg-surface2/60 px-3 py-1.5 text-xs text-muted"
      title={`Your account is a viewer: it can read everything here but not ${what}. An administrator can make it an operator.`}
    >
      <Eye className="h-3.5 w-3.5" aria-hidden="true" /> View only
    </span>
  )
}
