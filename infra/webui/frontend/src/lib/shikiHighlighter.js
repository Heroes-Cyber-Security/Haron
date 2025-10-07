import { createHighlighterCore } from 'shiki/core';
import { createOnigurumaEngine } from 'shiki/engine/oniguruma';
import wasm from 'shiki/wasm?module';

import vitesseLight from '@shikijs/themes/vitesse-light';
import vitesseDark from '@shikijs/themes/vitesse-dark';

import solidity from '@shikijs/langs/solidity';
import bash from '@shikijs/langs/bash';
import json from '@shikijs/langs/json';
import javascript from '@shikijs/langs/javascript';
import typescript from '@shikijs/langs/typescript';
import yaml from '@shikijs/langs/yaml';

const highlighter = await createHighlighterCore({
  themes: [vitesseLight, vitesseDark],
  langs: [solidity, bash, json, javascript, typescript, yaml],
  engine: createOnigurumaEngine(wasm),
});

export default highlighter;
