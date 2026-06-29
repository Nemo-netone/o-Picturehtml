const DEFAULT_UPSTREAM = 'https://sub.codexakihito.xyz';
const HOP_BY_HOP_HEADERS = new Set([
  'connection',
  'content-encoding',
  'content-length',
  'host',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
]);

export async function onRequest({ request, params }) {
  if (request.method === 'OPTIONS') {
    return new Response(null, {
      status: 204,
      headers: corsHeaders(),
    });
  }

  try {
    const upstreamBase = normalizeUpstream(request.headers.get('X-Picture-Upstream') || DEFAULT_UPSTREAM);
    const upstreamUrl = buildUpstreamUrl(upstreamBase, params?.path, request.url);
    const upstreamHeaders = buildUpstreamHeaders(request.headers);
    const init = {
      method: request.method,
      headers: upstreamHeaders,
      redirect: 'manual',
    };

    if (!['GET', 'HEAD'].includes(request.method.toUpperCase())) {
      init.body = request.body;
    }

    const upstreamResponse = await fetch(upstreamUrl, init);
    const responseHeaders = buildResponseHeaders(upstreamResponse.headers);
    return new Response(upstreamResponse.body, {
      status: upstreamResponse.status,
      statusText: upstreamResponse.statusText,
      headers: responseHeaders,
    });
  } catch (error) {
    return Response.json(
      { error: { message: error?.message || 'Proxy request failed' } },
      { status: 502, headers: corsHeaders() },
    );
  }
}

function normalizeUpstream(value) {
  const url = new URL(String(value || '').replace(/\/+$/, ''));
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    throw new Error('Invalid upstream protocol');
  }
  if (url.pathname.endsWith('/v1')) {
    url.pathname = url.pathname.slice(0, -3) || '/';
  }
  url.search = '';
  url.hash = '';
  return url;
}

function buildUpstreamUrl(upstreamBase, pathParam, requestUrl) {
  const path = Array.isArray(pathParam) ? pathParam.join('/') : String(pathParam || '');
  const target = new URL(`/v1/${path}`, upstreamBase);
  target.search = new URL(requestUrl).search;
  return target;
}

function buildUpstreamHeaders(sourceHeaders) {
  const headers = new Headers(sourceHeaders);
  headers.delete('X-Picture-Upstream');
  headers.delete('Origin');
  headers.delete('Referer');
  for (const header of HOP_BY_HOP_HEADERS) {
    headers.delete(header);
  }
  return headers;
}

function buildResponseHeaders(sourceHeaders) {
  const headers = new Headers();
  for (const [key, value] of sourceHeaders.entries()) {
    if (!HOP_BY_HOP_HEADERS.has(key.toLowerCase())) {
      headers.set(key, value);
    }
  }
  for (const [key, value] of corsHeaders().entries()) {
    headers.set(key, value);
  }
  return headers;
}

function corsHeaders() {
  return new Headers({
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET,POST,OPTIONS',
    'Access-Control-Allow-Headers': 'Authorization,Content-Type,Accept,X-Picture-Upstream',
    'Access-Control-Max-Age': '86400',
  });
}
