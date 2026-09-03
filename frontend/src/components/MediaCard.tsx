"use client";
import { useState } from "react";
import type { LinkCard } from "../lib/types";
export default function MediaCard({ card }: { card: LinkCard }) { const [expanded, setExpanded] = useState(false); const label = card.title || card.site_name || card.url;
  if (card.media_type === "video") return <div className="paper-card"><button type="button" onClick={() => setExpanded((v) => !v)}>{expanded ? "收起视频" : label}</button>{expanded && <iframe title={label} src={card.url} loading="lazy" allowFullScreen />}</div>;
  // Remote hosts are user/LLM supplied; next/image remotePatterns cannot safely be wildcarded here.
  if (card.media_type === "image" && card.image_url) return <a className="paper-card" href={card.url} target="_blank" rel="noreferrer"><img src={card.image_url} alt={label} loading="lazy" />{/* eslint-disable-line @next/next/no-img-element */}<span>{label}</span></a>;
  return <a className="paper-card" href={card.url} target="_blank" rel="noreferrer"><span>{label}</span>{card.description && <small>{card.description}</small>}</a>;
}
