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
