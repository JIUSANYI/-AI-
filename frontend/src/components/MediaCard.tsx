"use client";
import { useState } from "react";
import type { LinkCard } from "../lib/types";
export default function MediaCard({ card }: { card: LinkCard }) { const [expanded, setExpanded] = useState(false); const [imageFailed, setImageFailed] = useState(false); const label = card.title || card.site_name || card.url;
  if (card.media_type === "video") return <div className="media-card"><span className="media-type">VIDEO</span><button type="button" aria-label={`${expanded ? "收起" : "展开"}视频：${label}`} aria-expanded={expanded} onClick={() => setExpanded((v) => !v)}>{expanded ? "收起视频" : label}</button>{expanded && <iframe title={label} src={card.url} loading="lazy" allowFullScreen />}</div>;
  // Remote hosts are user/LLM supplied; next/image remotePatterns cannot safely be wildcarded here.
  if (card.media_type === "image" && card.image_url && !imageFailed) return <a className="media-card" href={card.url} target="_blank" rel="noreferrer"><span className="media-type">IMAGE</span><img src={card.image_url} alt={label} loading="lazy" onError={() => setImageFailed(true)} />{/* eslint-disable-line @next/next/no-img-element */}<span>{label}</span></a>;
  return <a className="media-card" href={card.url} target="_blank" rel="noreferrer"><span className="media-type">LINK</span><span>{label}</span>{card.description && <small>{card.description}</small>}</a>;
}
