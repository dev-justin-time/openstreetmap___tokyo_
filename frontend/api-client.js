/*
Minimal JS client to upload a GPX file to the Go gateway and receive the Rust JSON summary.
Usage: call uploadGpxFile(file)
*/
export async function uploadGpxFile(file) {
  const form = new FormData();
  form.append("gpx", file);
  const resp = await fetch("/upload", { method: "POST", body: form });
  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(text || "Upload failed");
  }
  return resp.json();
}
