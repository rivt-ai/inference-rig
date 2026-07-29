export function isTerminalDownloadState(state: string | undefined) {
  return state === 'completed' || state === 'failed' || state === 'cancelled' || state === 'already_downloaded';
}

export function canApplyDownload(state: string | undefined) {
  return state === 'completed' || state === 'already_downloaded';
}

export function chooseProfileSelection(
  profiles: { name: string }[],
  preferred: string,
  activeNames: string[],
  defaultProfile: string,
  selectedName: string
) {
  const names = profiles.map((profile) => profile.name);
  if (preferred && names.includes(preferred)) return preferred;
  const active = activeNames.find((name) => names.includes(name));
  if (active) return active;
  if (defaultProfile && names.includes(defaultProfile)) return defaultProfile;
  if (selectedName && names.includes(selectedName)) return selectedName;
  return names[0] || '';
}
