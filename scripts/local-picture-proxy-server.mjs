import { createServer } from 'node:http';
import { createReadStream, existsSync, statSync } from 'node:fs';
import { extname, join, resolve } from 'node:path';
import { Readable } from 'node:stream';
import { fileURLToPath } from 'node:url';

const PORT = Number(process.env.PORT || 8788);
const HOST = process.env.HOST || '127.0.0.1';
const DEFAULT_UPSTREAM = 'https://sub.codexakihito.xyz';
const SCRIPT_DIR = resolve(fileURLToPath(new URL('.', import.meta.url)));
const PROJECT_ROOT = resolve(SCRIPT_DIR, '..');
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

const CONTENT_TYPES = {
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.webp': 'image/webp',
};

const server = createServer(async (req, res) => {
  try {
    const requestUrl = new URL(req.url || '/', `http://${req.headers.host || `${HOST}:${PORT}`}`);
    if (requestUrl.pathname.startsWith('/v1/')) {
      await proxyApiRequest(req, res, requestUrl);
      return;
    }
    if (requestUrl.pathname === '/__picture_media') {
      await proxyMediaRequest(req, res, requestUrl);
      return;
    }

    serveStaticFile(requestUrl.pathname, res, {}, req.method);
  } catch (error) {
    res.writeHead(500, { 'Content-Type': 'application/json; charset=utf-8' });
    res.end(JSON.stringify({ error: error?.message || 'Local server error' }));
  }
});

server.listen(PORT, HOST, () => {
  console.log(`Picture local server: http://${HOST}:${PORT}/`);
});

async function proxyApiRequest(req, res, requestUrl) {
  if (req.method === 'OPTIONS') {
    res.writeHead(204, corsHeaders());
    res.end();
    return;
  }

  const upstreamBase = normalizeUpstream(req.headers['x-picture-upstream'] || DEFAULT_UPSTREAM);
  const upstreamUrl = new URL(requestUrl.pathname + requestUrl.search, upstreamBase);
  const headers = buildUpstreamHeaders(req.headers);
  const init = {
    method: req.method,
    headers,
    redirect: 'manual',
  };

  if (!['GET', 'HEAD'].includes(String(req.method).toUpperCase())) {
    init.body = req;
    init.duplex = 'half';
  }

  const upstreamResponse = await fetch(upstreamUrl, init);
  const responseHeaders = buildResponseHeaders(upstreamResponse.headers);
  res.writeHead(upstreamResponse.status, responseHeaders);

  if (!upstreamResponse.body) {
    res.end();
    return;
  }

  Readable.fromWeb(upstreamResponse.body).pipe(res);
}

async function proxyMediaRequest(req, res, requestUrl) {
  if (req.method === 'OPTIONS') {
    res.writeHead(204, corsHeaders());
    res.end();
    return;
  }

  if (!['GET', 'POST'].includes(String(req.method).toUpperCase())) {
    writeJson(res, 405, { error: 'Method not allowed' });
    return;
  }

  try {
    const payload = req.method === 'POST' ? await readJsonBody(req) : {};
    const target = requestUrl.searchParams.get('url') || payload.url;
    if (!target) {
      writeJson(res, 400, { error: 'Missing url' });
      return;
    }

    const targetUrl = new URL(target);
    if (targetUrl.protocol !== 'https:' && targetUrl.protocol !== 'http:') {
      writeJson(res, 400, { error: 'Invalid media url' });
      return;
    }

    if (targetUrl.hostname === HOST && targetUrl.port === String(PORT)) {
      serveStaticFile(targetUrl.pathname, res, corsHeaders());
      return;
    }

    const requestHeaders = {
      Accept: req.headers.accept || 'image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8',
    };
    if (req.headers.authorization) {
      requestHeaders.Authorization = req.headers.authorization;
    }

    const upstreamResponse = await fetch(targetUrl, {
      headers: requestHeaders,
      redirect: 'follow',
    });
    const contentType = upstreamResponse.headers.get('content-type') || '';

    if (!upstreamResponse.ok) {
      writeJson(res, upstreamResponse.status, await describeMediaUpstreamFailure(upstreamResponse, targetUrl));
      return;
    }

    if (contentType && !/^image\//i.test(contentType) && !/application\/octet-stream/i.test(contentType)) {
      writeJson(res, 502, await describeMediaUpstreamFailure(upstreamResponse, targetUrl, 'Media URL did not return an image'));
      return;
    }

    const responseHeaders = buildResponseHeaders(upstreamResponse.headers);
    if (!responseHeaders['content-type']) {
      responseHeaders['content-type'] = 'application/octet-stream';
    }
    res.writeHead(upstreamResponse.status, responseHeaders);

    if (!upstreamResponse.body) {
      res.end();
      return;
    }

    Readable.fromWeb(upstreamResponse.body).pipe(res);
  } catch (error) {
    writeJson(res, 502, {
      error: error?.message || 'Media proxy request failed',
      where: 'local-media-proxy',
    });
  }
}

function serveStaticFile(pathname, res, extraHeaders = {}, method = 'GET') {
  const relativePath = decodeURIComponent(pathname === '/' ? '/index.html' : pathname);
  const filePath = resolve(PROJECT_ROOT, `.${relativePath}`);
  if (!filePath.startsWith(PROJECT_ROOT)) {
    res.writeHead(403);
    res.end('Forbidden');
    return;
  }

  const finalPath = existsSync(filePath) && statSync(filePath).isDirectory()
    ? join(filePath, 'index.html')
    : filePath;

  if (!existsSync(finalPath) || !statSync(finalPath).isFile()) {
    res.writeHead(404);
    res.end('Not found');
    return;
  }

  res.writeHead(200, {
    'Content-Type': CONTENT_TYPES[extname(finalPath).toLowerCase()] || 'application/octet-stream',
    'Cache-Control': 'no-store',
    ...extraHeaders,
  });
  if (String(method).toUpperCase() === 'HEAD') {
    res.end();
    return;
  }

  const stream = createReadStream(finalPath);
  stream.on('error', (error) => {
    if (!res.headersSent) {
      res.writeHead(500, { 'Content-Type': 'application/json; charset=utf-8', ...corsHeaders() });
    }
    if (!res.writableEnded) {
      res.end(JSON.stringify({ error: error?.message || 'Static file stream failed' }));
    }
  });
  stream.pipe(res);
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

function buildUpstreamHeaders(sourceHeaders) {
  const headers = {};
  for (const [key, value] of Object.entries(sourceHeaders)) {
    const lowerKey = key.toLowerCase();
    if (HOP_BY_HOP_HEADERS.has(lowerKey)) continue;
    if (lowerKey === 'x-picture-upstream' || lowerKey === 'origin' || lowerKey === 'referer') continue;
    headers[key] = value;
  }
  return headers;
}

function buildResponseHeaders(sourceHeaders) {
  const headers = { ...corsHeaders() };
  sourceHeaders.forEach((value, key) => {
    if (!HOP_BY_HOP_HEADERS.has(key.toLowerCase())) {
      headers[key] = value;
    }
  });
  return headers;
}

function writeJson(res, status, data) {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', ...corsHeaders() });
  res.end(JSON.stringify(data));
}

function readJsonBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let total = 0;
    req.on('data', (chunk) => {
      total += chunk.length;
      if (total > 1024 * 1024) {
        reject(new Error('Media proxy body is too large'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      const text = Buffer.concat(chunks).toString('utf8').trim();
      if (!text) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(text));
      } catch {
        reject(new Error('Invalid media proxy JSON body'));
      }
    });
    req.on('error', reject);
  });
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
  return {
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET,POST,OPTIONS',
    'Access-Control-Allow-Headers': 'Authorization,Content-Type,Accept,X-Picture-Upstream',
    'Access-Control-Max-Age': '86400',
  };
}
