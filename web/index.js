"use strict";
const $ = (id) => document.getElementById(id);


const query = new URLSearchParams(location.search);
for (const [param, id] of [["firmware", "firmwareUrl"], ["config", "configUrl"], ["args", "extraArgs"]]) {
    if (query.has(param)) {
         $(id).value = query.get(param);
    }
}
for (const [param, id] of [["compression", "compression"], ["out", "outName"]]) {
    if ([...$(id).options].some((o) => o.value === query.get(param))) {
        $(id).value = query.get(param);
    }
}
for (const c of $("flags").querySelectorAll("input")) {
    c.checked = query.has(c.id);
}

function log(line, err) {
        const div = document.createElement("div");
        div.textContent = line;
        if (err) div.className = "text-danger";
        $("log").append(div);
        $("log").scrollTop = $("log").scrollHeight;
}

$("form").addEventListener("submit", async (ev) => {
        ev.preventDefault();
        $("log").replaceChildren();
        $("download").hidden = true;
        $("run").disabled = true;


        const files = [];
        const add = (name, buf) => {
            if (files.some((f) => f.name === name)) throw new Error("duplicate file name " + name);
            files.push({ name, buf });
            return "/in/" + name;
        };
       // the selected file, else the URL; the name is the last path segment
       const pick = async (input, url) => {
           if (input.files[0]) return add(input.files[0].name, await input.files[0].arrayBuffer());
           if (!url) return null;
           log("fetching " + url);
           let r;
           try {
               r = await fetch(url);
           } catch (e) {
               throw new Error(url + ": " + e.message + " (blocked by CORS or network error)");
           }
           if (!r.ok) throw new Error(url + ": HTTP " + r.status);
           const buf = await r.arrayBuffer();
           let name = decodeURIComponent(new URL(r.url).pathname.split("/").pop()) || "download";
           // firmware type is chosen by extension
           const ext = { PK: ".zip", MZ: ".exe" }[String.fromCharCode(...new Uint8Array(buf.slice(0, 2)))];
           if (ext && !name.toLowerCase().endsWith(ext)) name += ext;
           log("fetched " + name + " (" + (buf.byteLength / 1048576).toFixed(1) + " MiB)");
           return add(name, buf);
       };
        const outName = $("outName").value;
        let args;
        try {
                const fw = await pick($("firmware"), $("firmwareUrl").value.trim());
                if (!fw) throw new Error("no firmware selected");
                args = ["-in=" + fw];
                const config = await pick($("config"), $("configUrl").value.trim());
                if (config) args.push("-config=" + config);
                for (const f of $("extra").files) add(f.name, await f.arrayBuffer());
                if ($("compression").value) args.push("-compression=" + $("compression").value);
                for (const c of $("flags").querySelectorAll("input:checked")) args.push("-" + c.id);
                args.push(...$("extraArgs").value.split(/\s+/).filter(Boolean), "-out=/out/" + outName);
        } catch (e) {
                $("run").disabled = false;
                return log(e.message, true);
        }

        log("$ go ./cmd/repack-firmware " + args.join(" "));
        const start = Date.now();
        const timer = setInterval(() => ($("status").textContent = Math.round((Date.now() - start) / 1000) + " s"), 1000);

        const worker = new Worker("worker.js?@COMMIT_SHORT@");
        const finish = () => {
                clearInterval(timer);
                worker.terminate();
                $("run").disabled = false;
        };
        worker.onerror = (e) => {
                log("worker error: " + e.message, true);
                finish();
        };
        worker.onmessage = ({ data }) => {
                if (data.type === "log") return log(data.line, data.stream === "stderr");
                finish();
                if (data.code === 0 && data.data) {
                        const a = $("download");
                        a.href = URL.createObjectURL(new Blob([data.data], { type: "application/octet-stream" }));
                        a.download = outName;
                        a.textContent = "Download " + outName + " (" + (data.data.byteLength / 1048576).toFixed(1) + " MiB)";
                        a.hidden = false;
                } else {
                        log("exit code " + data.code + ", error", true);
                }
        };
        worker.postMessage({ files, args, outName }, files.map((f) => f.buf));
});