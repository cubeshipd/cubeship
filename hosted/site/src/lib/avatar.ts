// GitHub serves an avatar at whatever size is asked for; the default is 460px.
export function avatarAt(url: string, size: number): string {
  const avatar = new URL(url);
  avatar.searchParams.set("s", String(size));
  return avatar.toString();
}
