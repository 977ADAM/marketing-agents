/** Keep a request key while the outcome is unknown; editing input starts a new request. */
export function creationRequest<T>(send:(path:string,body:unknown,key:string)=>Promise<T>) {
 let previous='';let currentKey='';
 return async (path:string,body:unknown):Promise<T>=>{
  const input=path+JSON.stringify(body);
  if(input!==previous||!currentKey){previous=input;currentKey=crypto.randomUUID();}
  const key=currentKey;const result=await send(path,body,key);
  if(currentKey===key){currentKey='';previous='';}return result;
 };
}
