// tempUnschedErrorMessage.ts translates stored temporary-unscheduling reasons for provider views. Unknown reasons remain readable as their server values so new backend reasons do not become blank labels.
const reasonKeyPrefix = 'admin.providers.tempUnschedulable.reasons.'

export function displayTempUnschedErrorMessage(
  message: string | null | undefined,
  t: (key: string) => string,
  te: (key: string) => boolean,
): string {
  const raw = (message ?? '').trim()
  if (!raw) {
    return '-'
  }
  const key = `${reasonKeyPrefix}${raw}`
  if (te(key)) {
    return t(key)
  }
  return raw
}
