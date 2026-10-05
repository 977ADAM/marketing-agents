import {it,expect} from 'vitest';
import {createUploadCoordinator} from './uploads.js';
it('invalidates uploads for removed rows and keeps newer upload',()=>{const u=createUploadCoordinator();const old=u.start('a');expect(u.busy()).toBe(true);const newer=u.start('a');expect(old.current()).toBe(false);old.finish();expect(u.busy()).toBe(true);newer.finish();expect(u.busy()).toBe(false);const removed=u.start('b');u.remove('b');expect(removed.current()).toBe(false);expect(u.busy()).toBe(false);const last=u.start('a');u.destroy();expect(last.current()).toBe(false);});
