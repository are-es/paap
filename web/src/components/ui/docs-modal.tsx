"use client";

import { useState } from "react";
import { X, Copy, Check, BookOpen } from "lucide-react";
import { cn } from "@/lib/utils";

interface DocSection {
  title: string;
  content: string;
  code?: string;
  language?: string;
}

interface DocsModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  sections: DocSection[];
}

function CodeBlock({ code, language }: { code: string; language?: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="relative group">
      <pre className="p-3 rounded-lg bg-muted text-xs font-mono overflow-x-auto whitespace-pre-wrap break-all">
        {code}
      </pre>
      <button
        onClick={handleCopy}
        className="absolute top-2 right-2 p-1.5 rounded-md bg-background border border-input opacity-0 group-hover:opacity-100 transition-opacity"
        title="Copy"
      >
        {copied ? <Check className="w-3 h-3 text-neon-green" /> : <Copy className="w-3 h-3 text-muted-foreground" />}
      </button>
    </div>
  );
}

export function DocsModal({ open, onClose, title, sections }: DocsModalProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div className="relative bg-card border border-border rounded-xl shadow-xl w-[600px] max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border shrink-0">
          <div className="flex items-center gap-2">
            <BookOpen className="w-4 h-4 text-primary" />
            <h3 className="text-sm font-semibold">{title}</h3>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-accent transition-colors">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content */}
        <div className="p-4 space-y-4 overflow-y-auto flex-1">
          {sections.map((section, i) => (
            <div key={i} className="space-y-2">
              <h4 className="text-xs font-semibold text-foreground">{section.title}</h4>
              {section.content && (
                <p className="text-xs text-muted-foreground leading-relaxed whitespace-pre-line">{section.content}</p>
              )}
              {section.code && <CodeBlock code={section.code} language={section.language} />}
            </div>
          ))}
        </div>

        {/* Footer */}
        <div className="p-4 border-t border-border shrink-0 flex gap-2">
          <button
            onClick={() => {
              const text = sections.map(s => {
                let t = `## ${s.title}\n\n${s.content}`;
                if (s.code) t += `\n\n\`\`\`${s.language || ''}\n${s.code}\n\`\`\``;
                return t;
              }).join('\n\n---\n\n');
              navigator.clipboard.writeText(text);
            }}
            className="flex-1 px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground hover:opacity-90 transition-colors"
          >
            Copy All
          </button>
          <button onClick={onClose} className="flex-1 px-3 py-1.5 text-sm rounded-lg border border-input hover:bg-accent transition-colors">
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

// Docs button - reusable
export function DocsButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-lg border border-input hover:border-primary hover:text-primary transition-colors"
    >
      <BookOpen className="w-3.5 h-3.5" />
      Docs
    </button>
  );
}
