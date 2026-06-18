import { NS_PER_MS, NS_PER_SEC } from "../constants/units"

export function formatDuration(ns: number): string {
  const seconds = ns / NS_PER_SEC
  if (seconds < 1) return `${(ns / NS_PER_MS).toFixed(0)} ms`
  if (seconds < 60) return `${seconds.toFixed(2)} s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${(seconds % 60).toFixed(0)}s`
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

export function formatProcessTime(ns: number): string {
  return `${(ns / NS_PER_SEC).toFixed(3)}s`
}
