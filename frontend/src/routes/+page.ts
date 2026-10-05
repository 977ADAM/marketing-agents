export async function load({fetch}: {fetch: typeof globalThis.fetch}) {
 const response=await fetch('/api/limits');if (!response.ok) throw new Error('Не удалось загрузить ограничения');
 const limits=await response.json() as {max_topics:number};return {limits};
}
