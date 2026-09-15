export const PUBLIC_DEMO = process.env.NEXT_PUBLIC_CUBESHIP_DEMO === "1";
export const DEMO_PATH = PUBLIC_DEMO ? "/demo" : "";

export function startDownload(url: string, name: string) {
  if (!url.startsWith("blob:")) {
    window.location.assign(url);
    return;
  }
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${name}.demo.txt`;
  anchor.click();
}

export function demoDownloadURL(name: string): string {
  const url = URL.createObjectURL(
    new Blob(
      [
        `Cubeship demo: ${name}\nThis is a sample download. No real infrastructure or user data is connected.\n`,
      ],
      { type: "text/plain" },
    ),
  );
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
  return url;
}
