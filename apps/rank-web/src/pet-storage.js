const DB_NAME = "rank-companion";

/** Imported Codex atlases stay browser-local and are never sent to the Agent. */
export async function petStorage(action, value) {
  const db = await new Promise((resolve,reject) => {
    const request=indexedDB.open(DB_NAME,1);
    request.onupgradeneeded=()=>request.result.createObjectStore("pets");
    request.onsuccess=()=>resolve(request.result);request.onerror=()=>reject(request.error);
  });
  try { return await new Promise((resolve,reject)=>{
    const tx=db.transaction("pets",action==="get"?"readonly":"readwrite");const store=tx.objectStore("pets");
    const request=action==="get"?store.get("active"):action==="delete"?store.delete("active"):store.put(value,"active");
    tx.oncomplete=()=>resolve(request.result);tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error);
  }); } finally { db.close(); }
}

/** Accept only local PNG/WebP bytes matching the Codex v1 or v2 cell layout. */
export async function readPetFiles(files) {
  const manifestFile=[...files].find((f)=>f.name==="pet.json");
  if (!manifestFile || manifestFile.size>65536) throw new Error("invalid_pet");
  const manifest=JSON.parse(await manifestFile.text());const version=manifest.spriteVersionNumber??1;
  if (![1,2].includes(version) || typeof manifest.displayName!=="string" || !manifest.displayName.trim() || manifest.displayName.length>80 || typeof manifest.spritesheetPath!=="string" || /(^\/|\.\.|:|\\)/.test(manifest.spritesheetPath)) throw new Error("invalid_pet");
  const filename=manifest.spritesheetPath.split("/").at(-1);const file=[...files].find((f)=>f.name===filename || f.name.split("/").at(-1)===filename);
  const mime=file?.type || (filename.toLowerCase().endsWith(".png") ? "image/png" : filename.toLowerCase().endsWith(".webp") ? "image/webp" : "");
  if (!file || !["image/png","image/webp"].includes(mime) || file.size>12*1024*1024) throw new Error("invalid_pet");
  let width;let height;
  try { const bitmap=await createImageBitmap(file);width=bitmap.width;height=bitmap.height;bitmap.close(); }
  catch { const url=URL.createObjectURL(file);try { const image=await new Promise((resolve,reject)=>{const element=new Image();element.onload=()=>resolve(element);element.onerror=reject;element.src=url;});width=image.naturalWidth;height=image.naturalHeight;} finally {URL.revokeObjectURL(url);} }
  const valid=width===1536&&height===(version===2?2288:1872);
  if (!valid) throw new Error("invalid_pet");
  return {name:manifest.displayName,version,blob:file};
}
