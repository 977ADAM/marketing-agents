import type { Status } from '../api/client'

const LABELS: Record<Status, string> = {
  pending: 'В очереди',
  running: 'Генерация',
  done: 'Готово',
  failed: 'Ошибка',
}

export function StatusChip({ status }: { status: Status }) {
  return <span className={`chip chip-${status}`}>{LABELS[status]}</span>
}
