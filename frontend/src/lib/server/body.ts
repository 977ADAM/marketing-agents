export class BodyLimitError extends Error {}
/** Read a bounded request, including requests without Content-Length. */
export async function readBody(request: Request, limit: number): Promise<Uint8Array<ArrayBuffer>> {
 const length = Number(request.headers.get('content-length'));
 if (Number.isFinite(length) && length > limit) throw new BodyLimitError('Request body exceeds limit');
 const reader = request.body?.getReader();if (!reader) return new Uint8Array();
 const chunks: Uint8Array[] = [];let size=0;
 try {while (true) {
  const {done,value}=await reader.read();if (done) break;
  size+=value.byteLength;if (size>limit) {await reader.cancel();throw new BodyLimitError('Request body exceeds limit');}chunks.push(value);
 }} finally {reader.releaseLock();}
 const out=new Uint8Array(size);let offset=0;
 for (const chunk of chunks) {out.set(chunk,offset);offset+=chunk.length;}return out;
}
