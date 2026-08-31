import type { Status } from '../api/client'

const LABELS: Record<Status, string> = {
  pending: 'В очереди',
  running: 'Генерация',
  done: 'Готово',
  failed: 'Ошибка',
}

// runningLabel позволяет уточнить подпись для running (например «Проверка»).
export function StatusChip({ status, runningLabel }: { status: Status; runningLabel?: string }) {
  const label = status === 'running' && runningLabel ? runningLabel : LABELS[status]
  return <span className={`chip chip-${status}`}>{label}</span>
}
