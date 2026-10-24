const ID = /^\/dataset\/([a-z0-9-]+)$/;

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    let path;
    if (url.pathname === "/dataset") path = "/data/_catalog.json";
    else if (ID.test(url.pathname)) path = `/data/${ID.exec(url.pathname)[1]}.json`;
    else return env.ASSETS.fetch(request); // index.html and the SPA fallback

    const res = await env.ASSETS.fetch(new Request(new URL(path, url), request));
    // A missing asset comes back as index.html (SPA fallback); a revalidation comes back 304.
    const ct = res.headers.get("content-type") || "";
    if (res.status !== 304 && !ct.includes("json")) return new Response("not found", { status: 404 });
    return res; // etag, content-type, 304s come from Static Assets
  },
};
