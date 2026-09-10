import assert from "node:assert/strict";
import test from "node:test";
import { readPetFiles } from "../src/pet-storage.js";
import { gazeCell } from "../src/pet-animation.js";

test("Codex v2 gaze preserves clockwise cardinal directions and neutral dead zone", () => {
  assert.deepEqual(gazeCell(0,-100),{column:0,row:9});
  assert.deepEqual(gazeCell(100,0),{column:4,row:9});
  assert.deepEqual(gazeCell(0,100),{column:0,row:10});
  assert.deepEqual(gazeCell(-100,0),{column:4,row:10});
  assert.deepEqual(gazeCell(0,0),{column:6,row:0});
  for(let direction=0;direction<16;direction++){
    const angle=direction*Math.PI/8;
    assert.deepEqual(gazeCell(Math.sin(angle)*100,-Math.cos(angle)*100),{column:direction%8,row:9+Math.floor(direction/8)});
  }
});

test("pet imports accept local v1/v2 PNG/WebP atlases and reject invalid manifests or dimensions", async (t) => {
  const original=globalThis.createImageBitmap;
  t.after(()=>{globalThis.createImageBitmap=original;});
  let dimensions={width:1536,height:2288};let closed=0;
  globalThis.createImageBitmap=async()=>({...dimensions,close(){closed++;}});
  const manifest=(patch={})=>new File([JSON.stringify({displayName:"Rankling",spriteVersionNumber:2,spritesheetPath:"spritesheet.webp",...patch})],"pet.json",{type:"application/json"});
  const atlas=new File(["fixture"],"spritesheet.webp",{type:"image/webp"});
  assert.equal((await readPetFiles([manifest(),atlas])).version,2);
  dimensions.height=1872;
  assert.equal((await readPetFiles([manifest({spriteVersionNumber:1}),atlas])).version,1);
  for(const patch of [{spriteVersionNumber:3},{displayName:""},{spritesheetPath:"../spritesheet.webp"},{spritesheetPath:"https://example.com/spritesheet.webp"}]) await assert.rejects(readPetFiles([manifest(patch),atlas]));
  await assert.rejects(readPetFiles([manifest(),atlas]));
  await assert.rejects(readPetFiles([atlas]));
  await assert.rejects(readPetFiles([manifest()]));
  await assert.rejects(readPetFiles([new File(["{}"],"pet.json"),atlas]));
  await assert.rejects(readPetFiles([manifest(),new File(["x"],"spritesheet.webp",{type:"image/svg+xml"})]));
  assert.equal(closed,3);
});
