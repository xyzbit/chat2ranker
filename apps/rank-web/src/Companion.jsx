import { useEffect, useRef, useState } from "react";
import { ChatCircleDots } from "@phosphor-icons/react";
import { petStorage, readPetFiles } from "./pet-storage.js";
import { gazeCell } from "./pet-animation.js";
import { AssistantAvatar } from "./AssistantAvatar.jsx";

/** One persisted browser preference controls both the launcher and settings. */
export function useCompanion() {
  const [enabled,setEnabled]=useState(()=>localStorage.getItem("rank.companion.enabled")!=="false");
  const [pet,setPet]=useState({name:"Rank Signal",kind:"rank-signal"});
  const [error,setError]=useState("");
  useEffect(()=>{let stale=false;let url;petStorage("get").then((value)=>{if(value&&!stale){url=URL.createObjectURL(value.blob);setPet({...value,url});}}).catch(()=>{ setError("petStorageUnavailable"); });return()=>{stale=true;if(url)URL.revokeObjectURL(url);};},[]);
  useEffect(()=>{localStorage.setItem("rank.companion.enabled",String(enabled));},[enabled]);
  async function importFiles(files){setError("");try{const value=await readPetFiles([...files]);await petStorage("put",value);setPet((old)=>{if(old.url?.startsWith("blob:"))URL.revokeObjectURL(old.url);return {...value,url:URL.createObjectURL(value.blob)};});}catch{setError("petInvalid");}}
  async function reset(){setError("");try{await petStorage("delete");setPet((old)=>{if(old.url?.startsWith("blob:"))URL.revokeObjectURL(old.url);return {name:"Rank Signal",kind:"rank-signal"};});}catch{setError("petInvalid");}}
  return {enabled,setEnabled,pet,error,importFiles,reset};
}

/** Sprite timing follows the Codex state atlas; idle freezes for reduced motion. */
export function PetSprite({ pet, working=false, trackPointer=false }) {
  if (pet.kind === "rank-signal") return <AssistantAvatar className="rank-signal" trackPointer={trackPointer}/>;
  return <AtlasSprite pet={pet} working={working} trackPointer={trackPointer}/>;
}

function AtlasSprite({ pet, working, trackPointer }) {
  const [frame,setFrame]=useState(0); const [loaded,setLoaded]=useState(false);
  const element=useRef(null); const [gaze,setGaze]=useState(null);
  useEffect(()=>{
    if(!trackPointer || pet.version!==2 || working) {setGaze(null);return;}
    const media=matchMedia("(prefers-reduced-motion: reduce)");
    let animationFrame;
    function move(event){
      if(media.matches || event.pointerType!=="mouse")return;
      cancelAnimationFrame(animationFrame);
      animationFrame=requestAnimationFrame(()=>{
        const rect=element.current?.getBoundingClientRect();if(!rect)return;
        const next=gazeCell(event.clientX-rect.left-rect.width/2,event.clientY-rect.top-rect.height/2);
        setGaze((old)=>old?.row===next.row&&old?.column===next.column?old:next);
      });
    }
    const clear=()=>{cancelAnimationFrame(animationFrame);setGaze(null);};
    window.addEventListener("pointermove",move);document.documentElement.addEventListener("pointerleave",clear);media.addEventListener("change",clear);
    return()=>{clear();window.removeEventListener("pointermove",move);document.documentElement.removeEventListener("pointerleave",clear);media.removeEventListener("change",clear);};
  },[trackPointer,pet.version,working]);
  useEffect(()=>{setLoaded(false);const img=new Image();img.onload=()=>setLoaded(true);img.src=pet.url;return()=>{img.onload=null;};},[pet.url]);
  useEffect(()=>{const media=matchMedia("(prefers-reduced-motion: reduce)");let timer;let stopped=false;let index=0;const durations=working?[120,120,120,120,120,220]:[280,110,110,140,140,320];function tick(){if(stopped||media.matches)return;index=(index+1)%durations.length;setFrame(index);timer=setTimeout(tick,durations[index]);}function restart(){clearTimeout(timer);index=0;setFrame(0);if(!media.matches)timer=setTimeout(tick,durations[0]);}restart();media.addEventListener("change",restart);return()=>{stopped=true;clearTimeout(timer);media.removeEventListener("change",restart);};},[working,pet.url]);
  if(!loaded)return <ChatCircleDots size={32} weight="duotone"/>;
  const column=gaze?.column??frame;const row=working?7:gaze?.row??0;
  return <span ref={element} aria-hidden="true" className="pet-sprite" style={{backgroundImage:`url("${pet.url}")`,backgroundSize:`800% ${pet.version===2?1100:900}%`,backgroundPosition:`${column/7*100}% ${row/(pet.version===2?10:8)*100}%`}}/>;
}


export function CompanionLauncher({ companion, working, onOpen, t }) {
  if(!companion.enabled)return null;
  return <button className="companion-launcher" onClick={onOpen} aria-label={t("openAssistant")} title={`${companion.pet.name} · ${t(working?"assistantWorking":"assistantReady")}`}><PetSprite pet={companion.pet} working={working} trackPointer/><span className="companion-dot" data-working={working}/></button>;
}

export function CompanionSettings({ companion, t }) {
  const input=useRef(null);
  return <div className="companion-settings"><div className="companion-preview"><PetSprite pet={companion.pet}/><strong>{companion.pet.name}</strong></div><label className="check-label"><input type="checkbox" checked={companion.enabled} onChange={(e)=>companion.setEnabled(e.target.checked)}/>{t("showCompanion")}</label><p>{t("importPetHint")}</p><input hidden ref={input} type="file" multiple accept=".json,.png,.webp" onChange={(e)=>{void companion.importFiles(e.target.files);e.target.value="";}}/><div className="management-actions"><button onClick={()=>input.current?.click()}>{t("importPet")}</button><button onClick={companion.reset}>{t("resetPet")}</button></div>{companion.error && <p role="alert" className="management-error">{t(companion.error)}</p>}</div>;
}
