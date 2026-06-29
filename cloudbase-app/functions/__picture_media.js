const HOP_BY_HOP_HEADERS = new Set([
  'connection',
  'content-encoding',
  'content-length',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
]);

export async function onRequest({ request }) {
  if (request.method === 'OPTIONS') {
    return new Response(null, {
      status: 204,
      headers: corsHeaders(),
    });
  }

  try {
    if (!['GET', 'POST'].includes(request.method.toUpperCase())) {
      return Response.json({ error: 'Method not allowed' }, { status: 405, headers: corsHeaders() });
    }

    const requestUrl = new URL(request.url);
    const payload = request.method === 'POST' ? await readJsonBody(request) : {};
    const target = requestUrl.searchParams.get('url') || payload.url;
    if (!target) {
      return Response.json({ error: 'Missing url' }, { status: 400, headers: corsHeaders() });
    }

    const targetUrl = new URL(target);
    if (targetUrl.protocol !== 'https:' && targetUrl.protocol !== 'http:') {
      return Response.json({ error: 'Invalid media url' }, { status: 400, headers: corsHeaders() });
    }

    const requestHeaders = {
      Accept: request.headers.get('Accept') || 'image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8',
    };
    const authorization = request.headers.get('Authorization');
    if (authorization) {
      requestHeaders.Authorization = authorization;
    }

    const upstreamResponse = await fetch(targetUrl, {
      headers: requestHeaders,
      redirect: 'follow',
    });
    const contentType = upstreamResponse.headers.get('content-type') || '';

    if (!upstreamResponse.ok) {
      return Response.json(
        await describeMediaUpstreamFailure(upstreamResponse, targetUrl),
        { status: upstreamResponse.status, headers: corsHeaders() },
      );
    }

    if (contentType && !/^image\//i.test(contentType) && !/application\/octet-stream/i.test(contentType)) {
      return Response.json(
        await describeMediaUpstreamFailure(upstreamResponse, targetUrl, 'Media URL did not return an image'),
        { status: 502, headers: corsHeaders() },
      );
    }

    const responseHeaders = buildResponseHeaders(upstreamResponse.headers);
    return new Response(upstreamResponse.body, {
      status: upstreamResponse.status,
      statusText: upstreamResponse.statusText,
      headers: responseHeaders,
    });
  } catch (error) {
    return Response.json(
      { error: error?.message || 'Media proxy request failed' },
      { status: 502, headers: corsHeaders() },
    );
  }
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
  if (!headers.has('content-type')) {
    headers.set('content-type', 'application/octet-stream');
  }
  return headers;
}

async function readJsonBody(request) {
  const text = await request.text();
  if (!text.trim()) return {};
  try {
    return JSON.parse(text);
  } catch {
    throw new Error('Invalid media proxy JSON body');
  }
}

async function describeMediaUpstreamFailure(response, targetUrl, fallbackMessage = 'Media upstream request failed') {
  let body = '';
  try {
    body = await response.text();
  } catch {}

  return {
    error: fallbackMessage,
    upstreamStatus: response.status,
    upstreamStatusText: response.statusText,
    contentType: response.headers.get('content-type') || '',
    source: summarizeMediaSource(targetUrl),
    body: body.slice(0, 500),
  };
}

function summarizeMediaSource(url) {
  const path = url.pathname.length > 90 ? `${url.pathname.slice(0, 90)}...` : url.pathname;
  return `${url.origin}${path}`;
}

function corsHeaders() {
  return new Headers({
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET,POST,OPTIONS',
    'Access-Control-Allow-Headers': 'Authorization,Content-Type,Accept',
    'Access-Control-Max-Age': '86400',
  });
}
