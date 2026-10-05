import {it,expect,vi,afterEach} from 'vitest';
import {get} from 'svelte/store';
import type {Trajectory} from '#lib/api/types.js';
import {createTrajectoryStore} from './trajectory.js';
const page=(seq:number):Trajectory=>({id:'c',total:seq,events:[{seq} as Trajectory['events'][number]],next_seq:seq,has_more:false});
afterEach(()=>vi.useRealTimers());
it('polls every two seconds only while active and stops on destroy',async()=>{vi.useFakeTimers();const fetcher=vi.fn(async()=>page(1));const store=createTrajectoryStore('campaign','c',fetcher);await store.refresh();store.setActive(true);await vi.advanceTimersByTimeAsync(2000);expect(fetcher).toHaveBeenCalledTimes(2);store.setActive(false);await vi.advanceTimersByTimeAsync(4000);expect(fetcher).toHaveBeenCalledTimes(2);store.setActive(true);store.destroy();await vi.advanceTimersByTimeAsync(4000);expect(fetcher).toHaveBeenCalledTimes(2);});
it('appends cursor pages without duplicates',async()=>{const fetcher=vi.fn().mockResolvedValueOnce(page(1)).mockResolvedValueOnce(page(2));const store=createTrajectoryStore('review','r',fetcher);await store.refresh();await store.refresh();expect(fetcher.mock.calls[1][2]).toBe(1);expect(get(store.data)?.events.map(e=>e.seq)).toEqual([1,2]);store.destroy();});
