"use client";

import React, { useMemo, useRef, useEffect, useState } from "react";
import { Canvas, useFrame } from "@react-three/fiber";
import { OrbitControls } from "@react-three/drei";
import { EffectComposer, TiltShift2, Vignette, Bloom } from "@react-three/postprocessing";
import * as THREE from "three";

type CitySimProps = {
  isStrained: boolean;
  isAlgoActive: boolean;
  cloudCoverDetected?: boolean;
  heatmapMode?: boolean;
  anomalousNodes?: string[];
  recentTrades?: any[];
  onNodeClick?: (nodeData: any | null) => void;
};

function EnergyBeams({ trades, nodeDataList }: { trades: any[], nodeDataList: any[] }) {
  const getNodePos = (id: string) => {
    const node = nodeDataList.find((n: any) => n.id === id);
    return node ? new THREE.Vector3(node.position[0], 2, node.position[2]) : null;
  };

  return (
    <group>
      {trades.map((trade, idx) => {
        const start = getNodePos(trade.contributor_id);
        const end = getNodePos(trade.consumer_id);
        if (!start || !end) return null;

        const points = [start, new THREE.Vector3((start.x+end.x)/2, 10, (start.z+end.z)/2), end];
        const curve = new THREE.QuadraticBezierCurve3(points[0], points[1], points[2]);
        const tubeGeo = new THREE.TubeGeometry(curve, 20, 0.15, 8, false);

        return (
          <mesh key={idx} geometry={tubeGeo}>
            <meshStandardMaterial color="#10b981" emissive="#10b981" emissiveIntensity={3} transparent opacity={0.8} />
          </mesh>
        );
      })}
    </group>
  );
}

const random = (seed: number) => {
  const x = Math.sin(seed) * 10000;
  return x - Math.floor(x);
};

// Procedural texture for building windows
function createWindowTexture() {
  if (typeof document === "undefined") return null;
  const canvas = document.createElement("canvas");
  canvas.width = 256;
  canvas.height = 256;
  const ctx = canvas.getContext("2d");
  if (!ctx) return null;
  
  ctx.fillStyle = "#000000";
  ctx.fillRect(0, 0, 256, 256);
  ctx.fillStyle = "#ffffff";
  for (let y = 4; y < 256; y += 16) {
    for (let x = 4; x < 256; x += 16) {
      if (Math.random() > 0.4) ctx.fillRect(x, y, 8, 10);
    }
  }
  
  const tex = new THREE.CanvasTexture(canvas);
  tex.wrapS = THREE.RepeatWrapping;
  tex.wrapT = THREE.RepeatWrapping;
  tex.magFilter = THREE.NearestFilter;
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

const MassiveCity = React.memo(function MassiveCity({ 
  isStrained, 
  isAlgoActive, 
  heatmapMode,
  anomalousNodes = [],
  recentTrades = [],
  onNodeClick 
}: CitySimProps) {
  const buildingMeshRef = useRef<THREE.InstancedMesh>(null);
  const treeMeshRef = useRef<THREE.InstancedMesh>(null);

  const windowTexture = useMemo(() => createWindowTexture(), []);
  const buildingGeometry = useMemo(() => new THREE.BoxGeometry(1, 1, 1), []);
  const treeGeometry = useMemo(() => new THREE.SphereGeometry(1, 8, 8), []);

  const buildingMaterial = useMemo(() => {
    return new THREE.MeshStandardMaterial({
      color: "#94a3b8",
      roughness: 0.6,
      metalness: 0.3,
      emissiveMap: windowTexture || undefined,
      emissive: new THREE.Color(0x000000),
      emissiveIntensity: 0,
    });
  }, [windowTexture]);

  const treeMaterial = useMemo(() => new THREE.MeshStandardMaterial({ color: "#2d5a27", roughness: 0.9 }), []);

  const { buildingMatrices, treeMatrices, nodeDataList } = useMemo(() => {
    const gridSize = 25;
    const spacing = 1.4;
    const bPositions = [];
    const tPositions = [];
    const nodeDataList: any[] = [];
    let seed = 12345;
    
    for (let x = -gridSize; x <= gridSize; x++) {
      for (let z = -gridSize; z <= gridSize; z++) {
        const isRoadX = Math.abs(x) % 5 === 0;
        const isRoadZ = Math.abs(z) % 6 === 0;
        if (isRoadX || isRoadZ) {
          if (random(seed++) > 0.7) tPositions.push({ x, z, scale: 0.15 + random(seed++) * 0.2 });
          continue;
        }
        if (random(seed++) > 0.85) {
          for (let i = 0; i < 4; i++) {
            tPositions.push({ x: x + (random(seed++) - 0.5), z: z + (random(seed++) - 0.5), scale: 0.2 + random(seed++) * 0.3 });
          }
          continue;
        }
        bPositions.push({ x, z });
      }
    }
    
    const tempObj = new THREE.Object3D();
    const bMatrices = new Float32Array(bPositions.length * 16);
    for (let i = 0; i < bPositions.length; i++) {
      const { x, z } = bPositions[i];
      const sId = seed + i;
      const dist = Math.sqrt(x*x + z*z);
      const maxHeight = Math.max(1, 20 - dist * 0.8);
      const height = 0.5 + Math.pow(random(sId), 2) * maxHeight;
      const width = 0.6 + random(sId + 1) * 0.5;
      const depth = 0.6 + random(sId + 2) * 0.5;
      
      const px = x * spacing;
      const py = height / 2;
      const pz = z * spacing;

      tempObj.position.set(px, py, pz);
      tempObj.scale.set(width, height, depth);
      tempObj.updateMatrix();
      tempObj.matrix.toArray(bMatrices, i * 16);

      nodeDataList.push({
        id: `node_${Math.floor(random(sId + 5) * 500)}`, // Reduced ID space to match trades
        position: [px, height, pz],
        health: Math.floor(70 + random(sId + 6) * 30),
        temp: Math.floor(30 + random(sId + 7) * 45),
        earnings: (random(sId + 8) * 50).toFixed(2),
        consumption: random(sId + 9) * 10,
        replenishment: random(sId + 10) * 8,
      });
    }

    const tMatrices = new Float32Array(tPositions.length * 16);
    for (let i = 0; i < tPositions.length; i++) {
      const { x, z, scale } = tPositions[i];
      tempObj.position.set(x * spacing, scale / 2, z * spacing);
      tempObj.scale.set(scale, scale, scale);
      tempObj.updateMatrix();
      tempObj.matrix.toArray(tMatrices, i * 16);
    }
    
    return { buildingMatrices: bMatrices, treeMatrices: tMatrices, nodeDataList };
  }, []);

  useEffect(() => {
    if (buildingMeshRef.current) {
      buildingMeshRef.current.instanceMatrix.array = buildingMatrices;
      buildingMeshRef.current.instanceMatrix.needsUpdate = true;

      // Ensure colors array is sized correctly
      const colors = new Float32Array(nodeDataList.length * 3);
      for (let i = 0; i < colors.length; i++) colors[i] = 1;
      buildingMeshRef.current.instanceColor = new THREE.InstancedBufferAttribute(colors, 3);
    }
    if (treeMeshRef.current) {
      treeMeshRef.current.instanceMatrix.array = treeMatrices;
      treeMeshRef.current.instanceMatrix.needsUpdate = true;
    }
  }, [buildingMatrices, treeMatrices, nodeDataList.length]);

  useFrame((state) => {
    if (!buildingMeshRef.current) return;
    const mat = buildingMaterial;

    if (heatmapMode) {
      mat.emissiveIntensity = 0;
      mat.color.setHex(0x222222);
    } else if (isStrained && !isAlgoActive) {
      mat.color.setHex(0x94a3b8);
      mat.emissive.setHex(0x000000);
      mat.emissiveIntensity = 0;
    } else if (isStrained && isAlgoActive) {
      mat.color.setHex(0x94a3b8);
      const pulse = (Math.sin(state.clock.elapsedTime * 5) + 1) / 2;
      mat.emissive.setHex(0xffaa00).lerp(new THREE.Color(0xff1100), pulse);
      mat.emissiveIntensity = 2.0;
    } else {
      mat.color.setHex(0x94a3b8);
      mat.emissive.setHex(0xffddaa);
      mat.emissiveIntensity = 1.5;
    }

    // Instance Color updating
    const color = new THREE.Color();
    for (let i = 0; i < nodeDataList.length; i++) {
      const node = nodeDataList[i];
      let isAnomalous = anomalousNodes.includes(node.id);

      if (isAnomalous) {
        color.setHex(0xff0000); // Malicious = Red
      } else if (heatmapMode) {
        const ratio = node.consumption / (node.replenishment + 0.1);
        if (ratio > 1.2) color.setHex(0xff5500); // Orange = High consumption
        else color.setHex(0x10b981); // Emerald = Replenishing
      } else {
        color.setHex(0xffffff);
      }
      buildingMeshRef.current.setColorAt(i, color);
    }
    buildingMeshRef.current.instanceColor!.needsUpdate = true;
  });

  return (
    <group>
      <instancedMesh 
        ref={buildingMeshRef} 
        args={[buildingGeometry, buildingMaterial, buildingMatrices.length / 16]} 
        castShadow 
        receiveShadow 
        onClick={(e) => {
          e.stopPropagation();
          if (e.instanceId !== undefined && onNodeClick) {
            onNodeClick(nodeDataList[e.instanceId]);
          }
        }}
      />
      <instancedMesh 
        ref={treeMeshRef} 
        args={[treeGeometry, treeMaterial, treeMatrices.length / 16]} 
        castShadow 
        receiveShadow 
      />
      {recentTrades && recentTrades.length > 0 && (
        <EnergyBeams trades={recentTrades} nodeDataList={nodeDataList} />
      )}
    </group>
  );
});

function CinematicLighting({ isStrained, isAlgoActive, cloudCoverDetected }: CitySimProps) {
  const dirLightRef = useRef<THREE.DirectionalLight>(null);
  const ambientRef = useRef<THREE.AmbientLight>(null);

  useFrame(() => {
    if (!dirLightRef.current || !ambientRef.current) return;
    let targetSunColor = new THREE.Color(0xfff5e6);
    let targetSunIntensity = 2.0;
    let targetAmbientColor = new THREE.Color(0xffffff);
    let targetAmbientIntensity = 0.5;

    if (isStrained && !isAlgoActive) {
      targetSunColor = new THREE.Color(0x0a1020);
      targetSunIntensity = 0.05;
      targetAmbientColor = new THREE.Color(0x0a1020);
      targetAmbientIntensity = 0.02;
    } else if (isStrained && isAlgoActive) {
      targetSunColor = new THREE.Color(0xff4400);
      targetSunIntensity = 1.5;
      targetAmbientColor = new THREE.Color(0xff2200);
      targetAmbientIntensity = 0.2;
    } else if (cloudCoverDetected) {
      targetSunColor = new THREE.Color(0x8ba3b5); // Moody, overcast gray-blue
      targetSunIntensity = 0.5; // Simulate heavy clouds blocking sun
      targetAmbientColor = new THREE.Color(0x708090);
      targetAmbientIntensity = 0.3;
    }

    dirLightRef.current.color.lerp(targetSunColor, 0.05);
    dirLightRef.current.intensity = THREE.MathUtils.lerp(dirLightRef.current.intensity, targetSunIntensity, 0.05);
    ambientRef.current.color.lerp(targetAmbientColor, 0.05);
    ambientRef.current.intensity = THREE.MathUtils.lerp(ambientRef.current.intensity, targetAmbientIntensity, 0.05);
  });

  return (
    <>
      <ambientLight ref={ambientRef} intensity={0.5} color="#ffffff" />
      <directionalLight
        ref={dirLightRef}
        position={[40, 30, -30]}
        intensity={2.0}
        color="#fff5e6"
        castShadow
        shadow-mapSize-width={2048}
        shadow-mapSize-height={2048}
        shadow-camera-far={100}
        shadow-camera-left={-40}
        shadow-camera-right={40}
        shadow-camera-top={40}
        shadow-camera-bottom={-40}
        shadow-bias={-0.0005}
      />
    </>
  );
}

export default function CitySim({ 
  isStrained, 
  isAlgoActive, 
  cloudCoverDetected, 
  heatmapMode,
  anomalousNodes,
  recentTrades,
  onNodeClick 
}: CitySimProps) {
  const handleNodeClick = (node: any) => {
    if (onNodeClick) onNodeClick(node);
  };

  return (
    <Canvas shadows={{ type: THREE.PCFShadowMap }} camera={{ position: [40, 30, 40], fov: 25 }}>
      <color attach="background" args={["#000000"]} />
      
      <CinematicLighting isStrained={isStrained} isAlgoActive={isAlgoActive} cloudCoverDetected={cloudCoverDetected} />

      <mesh 
        rotation={[-Math.PI / 2, 0, 0]} 
        position={[0, 0, 0]} 
        receiveShadow
        onClick={() => handleNodeClick(null)} // Click ground to clear selection
      >
        <planeGeometry args={[200, 200]} />
        <meshStandardMaterial color="#1a1a1a" roughness={0.8} />
      </mesh>

      <MassiveCity 
        isStrained={isStrained} 
        isAlgoActive={isAlgoActive} 
        heatmapMode={heatmapMode}
        anomalousNodes={anomalousNodes}
        recentTrades={recentTrades}
        onNodeClick={handleNodeClick} 
      />

      {/* Manual User Controls Only */}
      <OrbitControls
        enableZoom={true}
        maxPolarAngle={Math.PI / 2 - 0.1}
        minDistance={5}
        maxDistance={120}
        autoRotate={false}
        target={[0, 0, 0]}
      />

      <EffectComposer disableNormalPass multisampling={4}>
        <TiltShift2 blur={0.12} />
        <Vignette eskil={false} offset={0.1} darkness={0.8} />
        <Bloom luminanceThreshold={0.5} intensity={0.8} mipmapBlur />
      </EffectComposer>
    </Canvas>
  );
}
