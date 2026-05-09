"use client";

import React, { useState } from "react";
import { Canvas } from "@react-three/fiber";
import { Html, OrbitControls, Sphere, useTexture } from "@react-three/drei";

type HubStatus = "NOMINAL" | "STRAIN";

type VppHub = {
  id: string;
  region: string;
  lat: number;
  lng: number;
  frequency: number;
  status: HubStatus;
};

const hubs: VppHub[] = [
  { id: "HUB-TYO-01", region: "Tokyo", lat: 35.6764, lng: 139.6500, frequency: 49.962, status: "NOMINAL" },
  { id: "HUB-NYC-03", region: "New York", lat: 40.7128, lng: -74.006, frequency: 49.884, status: "STRAIN" },
  { id: "HUB-LON-02", region: "London", lat: 51.5072, lng: -0.1276, frequency: 49.935, status: "NOMINAL" },
  { id: "HUB-DEL-04", region: "Delhi", lat: 28.6139, lng: 77.2090, frequency: 49.812, status: "STRAIN" },
  { id: "HUB-SYD-05", region: "Sydney", lat: -33.8688, lng: 151.2093, frequency: 49.971, status: "NOMINAL" },
];

function polarToCartesian(lat: number, lng: number, radius: number): [number, number, number] {
  const phi = (90 - lat) * (Math.PI / 180);
  const theta = (lng + 180) * (Math.PI / 180);
  const x = -(radius * Math.sin(phi) * Math.cos(theta));
  const y = radius * Math.cos(phi);
  const z = radius * Math.sin(phi) * Math.sin(theta);
  return [x, y, z];
}

function EarthMesh() {
  const [map, bumpMap, specularMap] = useTexture([
    "https://unpkg.com/three-globe/example/img/earth-blue-marble.jpg",
    "https://unpkg.com/three-globe/example/img/earth-topology.png",
    "https://unpkg.com/three-globe/example/img/earth-water.png",
  ]);

  return (
    <mesh>
      <sphereGeometry args={[2, 64, 64]} />
      <meshStandardMaterial map={map} bumpMap={bumpMap} bumpScale={0.06} metalnessMap={specularMap} metalness={0.35} roughness={0.7} />
    </mesh>
  );
}

function HubNodes() {
  const [activeHub, setActiveHub] = useState<string | null>(null);

  return (
    <>
      {hubs.map((hub) => {
        const position = polarToCartesian(hub.lat, hub.lng, 2.04);
        const color = hub.status === "STRAIN" ? "#f97316" : "#10b981";
        const isActive = activeHub === hub.id;

        return (
          <mesh
            key={hub.id}
            position={position}
            onPointerOver={() => setActiveHub(hub.id)}
            onPointerOut={() => setActiveHub((prev) => (prev === hub.id ? null : prev))}
            onClick={() => setActiveHub((prev) => (prev === hub.id ? null : hub.id))}
          >
            <sphereGeometry args={[0.02, 16, 16]} />
            <meshBasicMaterial color={color} />
            {isActive && (
              <Html center distanceFactor={8}>
                <div className="px-3 py-2 rounded-sm bg-black/80 border border-zinc-700 text-white text-xs whitespace-nowrap">
                  <div className="font-semibold">{hub.region}</div>
                  <div className="text-zinc-300">{hub.id}</div>
                  <div className={hub.status === "STRAIN" ? "text-orange-500" : "text-emerald-500"}>
                    {hub.frequency.toFixed(3)} Hz
                  </div>
                </div>
              </Html>
            )}
          </mesh>
        );
      })}
    </>
  );
}

export default function Globe() {
  return (
    <Canvas camera={{ position: [0, 0, 5.2], fov: 45 }}>
      <ambientLight intensity={0.1} />
      <directionalLight position={[5, 3, 5]} intensity={1.5} />
      <Sphere args={[2.02, 64, 64]}>
        <meshBasicMaterial color="#050505" transparent opacity={0.08} />
      </Sphere>
      <EarthMesh />
      <HubNodes />
      <OrbitControls enableZoom={true} autoRotate={true} autoRotateSpeed={0.5} />
    </Canvas>
  );
}
