import {it,expect} from 'vitest';
import {creationRequest} from './create.js';
it('reuses the key after a network error, changes it with input and after success',async()=>{
 const keys:string[]=[];let fail=true;
 const create=creationRequest(async (_path:string,_body:unknown,key:string)=>{keys.push(key);if(fail)throw new Error('network');return {id:'run'};});
 await expect(create('campaigns',{product:'P'})).rejects.toThrow('network');await expect(create('campaigns',{product:'P'})).rejects.toThrow('network');expect(keys[0]).toBe(keys[1]);
 fail=false;await create('campaigns',{product:'Q'});expect(keys[2]).not.toBe(keys[1]);await create('campaigns',{product:'Q'});expect(keys[3]).not.toBe(keys[2]);
});
