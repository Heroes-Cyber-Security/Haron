import { memo, useMemo } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeShikiFromHighlighter from '@shikijs/rehype/core';

import shikiHighlighter from '../lib/shikiHighlighter.js';

const Markdown = ({ content, className = '' }) => {
  const sanitizedContent = content?.trim();

  const rehypePlugins = useMemo(() => {
    const plugin = () => {
      const transformer = rehypeShikiFromHighlighter(shikiHighlighter, {
        themes: {
          light: 'vitesse-light',
          dark: 'vitesse-dark',
        },
        defaultColor: false,
      });

      return (tree) => transformer(tree);
    };

    return [plugin];
  }, []);

  if (!sanitizedContent) {
    return null;
  }

  return (
    <div className={`markdown ${className}`.trim()}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={rehypePlugins}
        components={{
          img: (props) => <img loading="lazy" decoding="async" {...props} />,
        }}
      >
        {sanitizedContent}
      </ReactMarkdown>
    </div>
  );
};

export default memo(Markdown);