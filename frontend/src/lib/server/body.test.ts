import {describe,it,expect} from 'vitest';
import {readBody,BodyLimitError} from './body.js';
describe('bounded request body',()=>{
 it('rejects oversized declared length',async()=>{const req=new Request('http://localhost',{method:'POST',headers:{'content-length':'11'},body:'x'});await expect(readBody(req,10)).rejects.toBeInstanceOf(BodyLimitError);});
 it('counts bytes without declared length',async()=>{const req=new Request('http://localhost',{method:'POST',body:'12345678901'});await expect(readBody(req,10)).rejects.toBeInstanceOf(BodyLimitError);});
 it('accepts exact boundary',async()=>{const req=new Request('http://localhost',{method:'POST',body:'1234567890'});expect(new TextDecoder().decode(await readBody(req,10))).toBe('1234567890');});
});
