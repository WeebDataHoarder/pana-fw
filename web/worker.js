"use strict";

const ERRNO = { SUCCESS: 0, BADF: 8, EXIST: 20, INVAL: 28, ISDIR: 31, NOENT: 44, NOSYS: 52, NOTDIR: 54, NOTSUP: 58 };
const FILETYPE = { CHAR: 2, DIR: 3, REG: 4 };
const OFLAGS = { CREAT: 1, DIRECTORY: 2, EXCL: 4, TRUNC: 8 };
const FDFLAGS_APPEND = 1;

class Exit {
       constructor(code) {
               this.code = code;
       }
}

class Wasi {
       // dirs: [[preopen path, Map(name -> {data: Uint8Array, size})]]
       constructor(args, env, dirs, log) {
               this.args = args;
               this.env = env;
               this.log = log;
               this.lines = { 1: "", 2: "" };
               this.decoder = new TextDecoder();
               this.fds = [{ type: FILETYPE.CHAR }, { type: FILETYPE.CHAR, out: 1 }, { type: FILETYPE.CHAR, out: 2 }];
               for (const [name, files] of dirs) {
                       this.fds.push({ type: FILETYPE.DIR, preopen: name, files });
               }
       }

       view() {
               return new DataView(this.memory.buffer);
       }

       bytes() {
               return new Uint8Array(this.memory.buffer);
       }

       setU64(view, ptr, value) {
               view.setUint32(ptr, value % 0x100000000, true);
               view.setUint32(ptr + 4, Math.floor(value / 0x100000000), true);
       }

       string(ptr, len) {
               return this.decoder.decode(this.bytes().slice(ptr, ptr + len));
       }

       writeStrings(list, ptrs, buf) {
               const view = this.view();
               const mem = this.bytes();
               const enc = new TextEncoder();
               for (const s of list) {
                       view.setUint32(ptrs, buf, true);
                       ptrs += 4;
                       const b = enc.encode(s);
                       mem.set(b, buf);
                       mem[buf + b.length] = 0;
                       buf += b.length + 1;
               }
               return ERRNO.SUCCESS;
       }

       sizeStrings(list, countPtr, sizePtr) {
               const view = this.view();
               const enc = new TextEncoder();
               view.setUint32(countPtr, list.length, true);
               view.setUint32(sizePtr, list.reduce((n, s) => n + enc.encode(s).length + 1, 0), true);
               return ERRNO.SUCCESS;
       }

       iovecs(iovs, n) {
               const view = this.view();
               const out = [];
               for (let i = 0; i < n; i++) {
                       out.push([view.getUint32(iovs + i * 8, true), view.getUint32(iovs + i * 8 + 4, true)]);
               }
               return out;
       }

       output(stream, data) {
               this.lines[stream] += this.decoder.decode(data, { stream: true });
               const parts = this.lines[stream].split("\n");
               this.lines[stream] = parts.pop();
               for (const line of parts) this.log(stream === 2 ? "stderr" : "stdout", line);
       }

       flush() {
               for (const stream of [1, 2]) {
                       if (this.lines[stream]) this.log(stream === 2 ? "stderr" : "stdout", this.lines[stream]);
                       this.lines[stream] = "";
               }
       }

       writeAt(file, pos, data) {
               const end = pos + data.length;
               if (end > file.data.length) {
                       const grown = new Uint8Array(Math.max(end, file.data.length * 2, 4096));
                       grown.set(file.data.subarray(0, file.size));
                       file.data = grown;
               }
               file.data.set(data, pos);
               file.size = Math.max(file.size, end);
       }

       lookup(dirfd, pathPtr, pathLen) {
               const dir = this.fds[dirfd];
               if (!dir) return { err: ERRNO.BADF };
               if (dir.type !== FILETYPE.DIR) return { err: ERRNO.NOTDIR };
               const name = this.string(pathPtr, pathLen).replace(/^(\.\/)+/, "").replace(/\/+$/, "");
               if (name === "" || name === ".") return { dir };
               if (name.includes("/")) return { err: ERRNO.NOENT };
               return { dir, name, file: dir.files.get(name) };
       }

       filestat(ptr, type, size) {
               const view = this.view();
               for (let i = 0; i < 64; i += 4) view.setUint32(ptr + i, 0, true);
               view.setUint8(ptr + 16, type);
               this.setU64(view, ptr + 24, 1);
               this.setU64(view, ptr + 32, size);
       }

       start(instance) {
               this.memory = instance.exports.memory;
               try {
                       instance.exports._start();
                       return 0;
               } catch (e) {
                       if (e instanceof Exit) return e.code;
                       throw e;
               } finally {
                       this.flush();
               }
       }

       imports() {
               const unsigned = (fn) => (...args) => fn(...args.map((a) => (typeof a === "number" ? a >>> 0 : a)));
               const fns = this.functions();
               for (const name in fns) fns[name] = unsigned(fns[name]);
               return fns;
       }

       functions() {
               return {
                       args_get: (argv, buf) => this.writeStrings(this.args, argv, buf),
                       args_sizes_get: (count, size) => this.sizeStrings(this.args, count, size),
                       environ_get: (env, buf) => this.writeStrings(this.env, env, buf),
                       environ_sizes_get: (count, size) => this.sizeStrings(this.env, count, size),
                       clock_time_get: (id, precision, ptr) => {
                               this.setU64(this.view(), ptr, Math.round((id === 0 ? Date.now() : performance.timeOrigin + performance.now()) * 1e6));
                               return ERRNO.SUCCESS;
                       },
                       random_get: (ptr, len) => {
                               const mem = this.bytes();
                               for (let i = 0; i < len; i += 65536) crypto.getRandomValues(mem.subarray(ptr + i, ptr + Math.min(len, i + 65536)));
                               return ERRNO.SUCCESS;
                       },
                       sched_yield: () => ERRNO.SUCCESS,
                       proc_exit: (code) => {
                               throw new Exit(code);
                       },

                       poll_oneoff: (inPtr, outPtr, n, neventsPtr) => {
                               const mem = this.bytes();
                               for (let i = 0; i < n; i++) {
                                       const sub = inPtr + i * 48;
                                       const ev = outPtr + i * 32;
                                       mem.fill(0, ev, ev + 32);
                                       // userdata
                                       mem.copyWithin(ev, sub, sub + 8);
                                       // type
                                       mem[ev + 10] = mem[sub + 8];
                               }
                               this.view().setUint32(neventsPtr, n, true);
                               return ERRNO.SUCCESS;
                       },
                       fd_write: (fd, iovs, n, nwritten) => {
                               const f = this.fds[fd];
                               if (!f) return ERRNO.BADF;
                               let total = 0;
                               for (const [ptr, len] of this.iovecs(iovs, n)) {
                                       const data = this.bytes().slice(ptr, ptr + len);
                                       if (f.out) this.output(f.out, data);
                                       else if (f.file) {
                                               this.writeAt(f.file, f.append ? f.file.size : f.pos, data);
                                               f.pos = (f.append ? f.file.size : f.pos + len);
                                       } else return ERRNO.BADF;
                                       total += len;
                               }
                               this.view().setUint32(nwritten, total, true);
                               return ERRNO.SUCCESS;
                       },
                       fd_read: (fd, iovs, n, nread) => {
                               const f = this.fds[fd];
                               if (!f) return ERRNO.BADF;
                               let total = 0;
                               if (f.file) {
                                       for (const [ptr, len] of this.iovecs(iovs, n)) {
                                               const chunk = f.file.data.subarray(f.pos, Math.min(f.file.size, f.pos + len));
                                               this.bytes().set(chunk, ptr);
                                               f.pos += chunk.length;
                                               total += chunk.length;
                                               if (chunk.length < len) break;
                                       }
                               } else if (f.type === FILETYPE.DIR) return ERRNO.ISDIR;
                               this.view().setUint32(nread, total, true);
                               return ERRNO.SUCCESS;
                       },
                       fd_pread: (fd, iovs, n, offset, nread) => {
                               const f = this.fds[fd];
                               if (!f?.file) return ERRNO.BADF;
                               let pos = Number(offset);
                               let total = 0;
                               for (const [ptr, len] of this.iovecs(iovs, n)) {
                                       const chunk = f.file.data.subarray(pos, Math.min(f.file.size, pos + len));
                                       this.bytes().set(chunk, ptr);
                                       pos += chunk.length;
                                       total += chunk.length;
                                       if (chunk.length < len) break;
                               }
                               this.view().setUint32(nread, total, true);
                               return ERRNO.SUCCESS;
                       },
                       fd_seek: (fd, offset, whence, newOffset) => {
                               const f = this.fds[fd];
                               if (!f?.file) return ERRNO.BADF;
                               const base = [0, f.pos, f.file.size][whence];
                               const pos = base + Number(offset);
                               if (base === undefined || pos < 0) return ERRNO.INVAL;
                               f.pos = pos;
                               this.setU64(this.view(), newOffset, pos);
                               return ERRNO.SUCCESS;
                       },
                       fd_close: (fd) => {
                               if (!this.fds[fd]) return ERRNO.BADF;
                               delete this.fds[fd];
                               return ERRNO.SUCCESS;
                       },
                       fd_filestat_get: (fd, ptr) => {
                               const f = this.fds[fd];
                               if (!f) return ERRNO.BADF;
                               this.filestat(ptr, f.file ? FILETYPE.REG : f.type, f.file ? f.file.size : 0);
                               return ERRNO.SUCCESS;
                       },
                       fd_fdstat_get: (fd, ptr) => {
                               const f = this.fds[fd];
                               if (!f) return ERRNO.BADF;
                               const view = this.view();
                               for (let i = 0; i < 24; i += 4) view.setUint32(ptr + i, 0, true);
                               view.setUint8(ptr, f.file ? FILETYPE.REG : f.type);
                               for (let i = 8; i < 24; i += 4) view.setUint32(ptr + i, 0xffffffff, true);
                               return ERRNO.SUCCESS;
                       },
                       // set not pollable so Go uses blocking reads and writes
                       fd_fdstat_set_flags: (fd) => (this.fds[fd] ? ERRNO.NOTSUP : ERRNO.BADF),
                       fd_prestat_get: (fd, ptr) => {
                               const f = this.fds[fd];
                               if (!f?.preopen) return ERRNO.BADF;
                               const view = this.view();
                               view.setUint32(ptr, 0, true);
                               view.setUint32(ptr + 4, new TextEncoder().encode(f.preopen).length, true);
                               return ERRNO.SUCCESS;
                       },
                       fd_prestat_dir_name: (fd, ptr, len) => {
                               const f = this.fds[fd];
                               if (!f?.preopen) return ERRNO.BADF;
                               this.bytes().set(new TextEncoder().encode(f.preopen).subarray(0, len), ptr);
                               return ERRNO.SUCCESS;
                       },
                       fd_readdir: () => ERRNO.NOSYS,
                       path_filestat_get: (dirfd, flags, pathPtr, pathLen, ptr) => {
                               const r = this.lookup(dirfd, pathPtr, pathLen);
                               if (r.err) return r.err;
                               if (r.name === undefined) this.filestat(ptr, FILETYPE.DIR, 0);
                               else if (r.file) this.filestat(ptr, FILETYPE.REG, r.file.size);
                               else return ERRNO.NOENT;
                               return ERRNO.SUCCESS;
                       },
                       path_open: (dirfd, dirflags, pathPtr, pathLen, oflags, rightsBase, rightsInheriting, fdflags, fdPtr) => {
                               const r = this.lookup(dirfd, pathPtr, pathLen);
                               if (r.err) return r.err;
                               let entry;
                               if (r.name === undefined) {
                                       entry = { type: FILETYPE.DIR, files: r.dir.files };
                               } else {
                                       if (oflags & OFLAGS.DIRECTORY) return r.file ? ERRNO.NOTDIR : ERRNO.NOENT;
                                       let file = r.file;
                                       if (file && oflags & OFLAGS.EXCL && oflags & OFLAGS.CREAT) return ERRNO.EXIST;
                                       if (!file) {
                                               if (!(oflags & OFLAGS.CREAT)) return ERRNO.NOENT;
                                               file = { data: new Uint8Array(0), size: 0 };
                                               r.dir.files.set(r.name, file);
                                       }
                                       if (oflags & OFLAGS.TRUNC) file.size = 0;
                                       entry = { type: FILETYPE.REG, file, pos: 0, append: (fdflags & FDFLAGS_APPEND) !== 0 };
                               }
                               let fd = 3;
                               while (this.fds[fd]) fd++;
                               this.fds[fd] = entry;
                               this.view().setUint32(fdPtr, fd, true);
                               return ERRNO.SUCCESS;
                       },
                       path_unlink_file: (dirfd, pathPtr, pathLen) => {
                               const r = this.lookup(dirfd, pathPtr, pathLen);
                               if (r.err) return r.err;
                               if (!r.file) return ERRNO.NOENT;
                               r.dir.files.delete(r.name);
                               return ERRNO.SUCCESS;
                       },
                       path_remove_directory: () => ERRNO.NOTSUP,
               };
       }
}

let compiled;

onmessage = (ev) => {
       const { files, args, outName } = ev.data;
       const log = (stream, line) => postMessage({ type: "log", stream, line });
       const inDir = new Map(files.map((f) => [f.name, { data: new Uint8Array(f.buf), size: f.buf.byteLength }]));
       const outDir = new Map();
       const wasi = new Wasi(["repack-firmware", ...args], ["GOMEMLIMIT=1GiB"], [["/in", inDir], ["/out", outDir]], log);

       (compiled ? Promise.resolve(compiled) : fetch("repack-firmware.wasm" + location.search).then((r) => r.arrayBuffer()).then((b) => WebAssembly.compile(b)))
               .then((m) => {
                       compiled = m;
                       return WebAssembly.instantiate(m, { wasi_snapshot_preview1: wasi.imports() });
               })
               .then((instance) => {
                       const code = wasi.start(instance);
                       const out = outDir.get(outName);
                       if (code === 0 && out && out.size > 0) {
                               const data = out.data.buffer.byteLength === out.size ? out.data.buffer : out.data.slice(0, out.size).buffer;
                               postMessage({ type: "done", code, data }, [data]);
                       } else {
                               postMessage({ type: "done", code });
                       }
               })
               .catch((e) => {
                       log("stderr", String((e && e.stack) || e));
                       postMessage({ type: "done", code: -1 });
               });
};
