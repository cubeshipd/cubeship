# Gemini GitHub Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar três blocos independentes: diagnóstico Gemini manual no resumo do run, CI agregado determinístico e pin/smoke limitado dos templates.

**Architecture:** Actions com scripts confiáveis e jobs separados; nenhum serviço externo além de GitHub/Gemini. Implementar primeiro offline no Mac desta tarefa, usando clones isolados e respostas simuladas. Não há ledger, app próprio, publicação no PR ou execução de PR com chave Gemini.

**Tech Stack:** Node 24, `node:test`, `fetch` e bibliotecas padrão; REST GitHub/Gemini; YAML Actions. Smoke usa Docker e o pacote Go `cubeship/template` já existente, sem nova biblioteca YAML.

**Spec:** [2026-10-11-gemini-github-automation-design.md](../specs/2026-10-11-gemini-github-automation-design.md), aprovada por Lucas em 21:17 UTC. Plano aprovado por Lucas às 21:29 UTC. O registro de execução mantém os checkpoints abaixo separados da especificação.

## Estado e autorizações de execução

Lucas aprovou execução contínua com `superpowers:subagent-driven-development`, TDD, revisões por tarefa/bloco e revisão final. Autorizou dependências de desenvolvimento apenas no espaço isolado, commits locais, branches de trabalho, push dessas branches e PRs draft para validar CI. Essa autorização substitui as restrições históricas sobre instalação/publicação local abaixo. Não autoriza merge, push em branch compartilhada, release, billing, grants, branch rules nem leitura/configuração de credenciais.

Às 21:33 UTC, Lucas priorizou **somente Cubeship**, validando o core antes de replicar para templates/Jian. Pin e smoke de templates ficam adiados. Lucas adicionou o secret; esta tarefa verifica somente sua presença por metadados. Uma chamada pública pequena só poderá ocorrer em workflow confiável e permitido, sem contornar a exigência de dispatch na branch padrão. `liveEnabled` pode permanecer falso.

O master mobile foi integrado ao clone isolado preservando as alterações do piloto. Notas de release e a PR externa de dependência ficam fora desta implementação. Checkpoints e limitações finais constarão em `.github/pilot.md` e no relatório da entrega.

## Global Constraints

- Ambiente de implementação: esta tarefa no Mac. Preservar alterações existentes; somente clones isolados `cubeship`, `cubeship-templates`, `jian`. Revalidar `AGENTS.md` e `.agents/skills` ao iniciar; nenhuma infraestrutura nova.
- Manual por mantenedor `maintain`/`admin`, branch padrão, PR aberto e SHA explícito; somente conteúdo público. Sem labels/tradução/deduplicação de issues, comentários/checks de IA no PR, aprovação, merge, push, correção ou publicação automática.
- Scripts/actions confiáveis pinados; nunca executar PR, instruções ou URLs contidas no diff na trilha Gemini. Sem logs brutos, prompt/diff/resposta bruta em logs/artefatos; artefato somente JSON público filtrado, retenção **um dia**.
- Entrada: **20 arquivos, 1.000 linhas modificadas, 128 KiB, 12.000 tokens totais**. Excesso ou patch incompleto: incompleto, sem chunking. Saída: **maxOutputTokens=4096, 8 KiB JSON, três achados, 4.000 caracteres de resumo**.
- Uma tentativa de geração/run; **90 s**, nenhum retry automático, inclusive 429/5xx/timeout. Leituras GitHub: **até dois retries com backoff**. Diagnóstico: **10 min**, um run/repo; idempotência por SHA **best-effort**, não exatamente uma chamada.
- Smoke: **dois alvos no máximo, 10 min/imagem, probe 30 s, 1 CPU/512 MiB/128 PIDs**. Sem volumes, serviços reais, portas públicas ou socket do host; fork em runner hospedado efêmero sem secrets/escrita/credenciais persistidas/cache privilegiado.
- Revisão: **gemini-3.8-flash**, thinking **low**, configuração confiável; **gemini-3.5-flash-lite** fica fora da execução, reservado à triagem futura.
- Meta conjunta **US$ 5/mês**, até **100 análises/mês** acompanhadas manualmente; sem quota global/hard cap garantido. Projeto API dedicado, chave, Paid/billing, permissões mínimas e limite/alertas do provedor só após autorização separada. Código privado permanece excluído; nunca fallback Free.
- Sem alterações de branch protection, instalação de dependências, push ou PR durante execução local sem autorização correspondente. Commits locais apenas dos arquivos do bloco e após checks exigidos pelo repo; se dependerem de instalação não autorizada, deixar alterações revisáveis sem commit e informar.

## Review Focus

1. Diff paginado com rename, symlink, binário ou patch ausente: não alegar cobertura completa — A1.
2. Ator de rerun diferente, acesso revogado, histórico removido ou HEAD/base mudando: recusar/obsoleto, sem nova geração automática — A1/A2.
3. Resposta HTTP de erro contendo chave/prompt ou JSON válido com código/HTML malicioso: não vazar em log/artefato/resumo — A2.
4. Gate verde por skip ou check de outro SHA: impedir sucesso sem jobs essenciais completos — B1/A1.
5. Tag redirecionada a registry arbitrário, Dockerfile modificado ou VOLUME herdado: sem credenciais/mounts/runtime indevido — C2/C3.

## Arquivos e entregáveis

Todos os paths seguintes são relativos ao repo indicado. Não modificar Makefiles, código de produto, lockfiles ou workflows de release.

| Bloco | Arquivos criados/modificados |
| --- | --- |
| A — diagnóstico | Em cada repo: criar `.github/scripts/pilot-core.mjs`, `pilot-run.mjs`, `pilot.test.mjs`, `pilot-config.json`; criar `.github/workflows/gemini-pilot.yml` e `.github/pilot.md` |
| B — agregado | Modificar `cubeship/.github/workflows/ci.yml`, `jian/.github/workflows/ci.yml`, `cubeship-templates/.github/workflows/check.yml`; criar em cada repo `.github/scripts/ci-aggregate.mjs` e `ci-aggregate.test.mjs` |
| C — templates | Modificar apenas checkout do validador em `.github/workflows/check.yml`; criar `.github/scripts/template-smoke.mjs`, `template-smoke.test.mjs`, `smoke-source.go`, `smoke-source_test.go`, `.github/workflows/template-smoke.yml` e `.github/template-smoke.md` |

Não criar pasta raiz de testes/docs nos templates: `templatecheck` trata diretórios raiz como templates. A fica inicialmente em Cubeship; após testes, copiar seus scripts/testes byte a byte para os outros dois repos. Três cópias locais pequenas evitam workflow compartilhado, publicação prévia ou pacote novo; conferir igualdade ao fechar A. Documentação/nomes de produto em inglês, conforme Jian.

## A — Diagnóstico Gemini manual

### A1. Snapshot, autorização e filtragem

**Files:** Cubeship `.github/scripts/pilot-core.mjs`, `pilot.test.mjs`, `pilot-config.json`.

**Interfaces:** `Context={repo,ref,defaultBranch,actor,triggeringActor,runId,runAttempt,sha,workflowSha,historyComplete}`; `PR={number,state,isPublic,head,base}`; `permissions` mapeia cada ator à permissão retornada pela API. `Snapshot={repo,pr,head,base,runId,workflowSha}`; `FileChange={path,previousPath,mode,status,additions,deletions,patch}`; `Decision={state:'ready'|'blocked'|'incomplete'|'obsolete',reason}`. Exportar `authorize(context,pr,permissions):Decision`, `checkHistory(snapshot,runs):Decision`, `prepareInput(snapshot,files):{decision,prompt,changedLines}` e `sameSnapshot(expected,current):boolean`. `changedLines` mapeia path e lado `LEFT|RIGHT` para linhas alteradas. Configuração contém allowlist dos três repos/modelo, limites acima e `liveEnabled:false`; nenhum override de modelo pelo dispatch.

- [ ] **Testes vermelhos:** em `pilot.test.mjs`, `authorization` exige branch padrão, ambos atores maintain/admin, repo público, SHA de 40 hex e PR aberto; `input_boundaries` aceita exatamente 20/1.000/131072 e recusa 21/1.001/131073; `incomplete_diff` recusa paginação incompleta, patch truncado/ausente e symlink. `history` ignora o próprio run, bloqueia outro run do mesmo repo/PR/SHA, rerun e histórico indisponível. `injection` preserva texto como dados sem executar shell/URLs; `.env*`, chaves/certificados/dumps/gerados e padrão de secret bloqueiam antes de envio. `snapshot` retorna false para mudança de head ou base.
  Assertions centrais: `assert.equal(authorize(ctx, {...pr, isPublic:false}, perms).state, 'blocked')`; `assert.equal(sameSnapshot(snapshot, {...snapshot, base:otherSha}), false)`.
- [ ] **Provar falha:** `node --test .github/scripts/pilot.test.mjs`; esperado FAIL por exports ausentes, não por fixture inválida.
- [ ] **Implementar contratos:** validar paths sem traversal/controles e modo Git via tree API; conferir soma de hunks contra additions/deletions. Contar arquivos/linhas do diff inteiro antes de excluir paths. Prompt fixo instrui diagnóstico consultivo e trata diff como dados; nunca carregar skills do PR. Sem logs de payload.
- [ ] **Provar passagem:** mesmo comando, exit 0, incluindo todas as fronteiras e fixtures sem rede/secrets.
- [ ] **Checkpoint:** revisar diff e criar commit local `Add guarded public PR input preparation` somente se checks do repo forem possíveis sem instalação não autorizada.

### A2. REST, resposta segura e resumo

**Files:** Cubeship `.github/scripts/pilot-run.mjs`, `pilot-core.mjs`, `pilot.test.mjs`.

**Interfaces:** consumir A1; exportar `githubRead(path,transport):Promise<JSON>`, `collectSnapshot(context,transport):Promise<Snapshot>`, `generateOnce(input,transport,key):Promise<{state,result?,usage?}>`, `validateResult(json,changedLines,snapshot):Decision`, `renderSummary(result,snapshot):string`. `input` contém snapshot/prompt/changedLines de A1 e configuração confiável; `usage` contém somente contagens numéricas, estado de medição e custo estimado. CLI: `collect`, `infer`, `summarize`; transporte/fetch/clock injetáveis para teste. Resultado fechado: `snapshot`, `coverage:'complete'|'incomplete'`, `summary`, `findings[{severity,path,side,line,explanation}]`; severidades `low|medium|high`. Resumo sempre contém repo/PR/head/base e aviso de hipótese.

- [ ] **Testes vermelhos:** `github_retries` exige no máximo três tentativas totais de leitura e `Retry-After`; `one_generation` exige exatamente uma chamada GenerateContent em sucesso/429/5xx/AbortError. `token_gate` recusa 12001 e aceita 12000 antes da geração. `response_gate` rejeita MAX_TOKENS, tool/functionCall, extras, quatro achados, JSON >8192 bytes, linha inexistente, fence de código/secret/HTML e cópia de trecho do diff. `artifact` contém apenas JSON filtrado + IDs/usage, nunca prompt/header/key; `render_limit` garante ≤4000 caracteres e links gerados pelo SHA. `obsolete` gera zero chamadas quando snapshot mudou antes da API. Capturar stdout/stderr e provar ausência de sentinelas secret/prompt inclusive em corpo de erro HTTP.
  Assertions: `assert.equal(transport.generationCalls, 1)` após timeout; `assert.ok(renderSummary(result, snapshot).length <= 4000)`; `assert.equal(validateResult({...result, extra:true}, lines, snapshot).state, 'blocked')`.
- [ ] **Provar falha:** `node --test .github/scripts/pilot.test.mjs`; novos casos FAIL pelos contratos ausentes.
- [ ] **Implementar REST mínimo:** montar URLs GitHub a partir de repo/PR validados, paginar completamente, sem baixar logs; escolher CI de workflow/commit correto, incluindo pais do merge sintético. Inferência refaz leitura pública sem token; filtra antes de `countTokens` e inclui systemInstruction na contagem. GenerateContent único, `candidateCount:1`, `responseMimeType:'application/json'`, `responseJsonSchema` fechado, `thinkingConfig:{thinkingLevel:'LOW',includeThoughts:false}`, `maxOutputTokens:4096`, sem tools/cache. Abort em 90 s; desativar qualquer retry do cliente. Erros logam códigos fixos, nunca bodies. Artefato só após validação conservadora e filtragem; remover temporários em finally. `summarize` verifica origem/run/SHA, refaz referências públicas e reconsulta HEAD/base antes de escrever `GITHUB_STEP_SUMMARY`.
- [ ] **Provar passagem:** mesmo comando, exit 0. Fixtures devem cobrir `usageMetadata.promptTokenCount`, `candidatesTokenCount`, `thoughtsTokenCount` e `totalTokenCount`: pensamento separado também entra no custo; ausência/divergência marca medição incerta. Não interpretar 4096 como teto financeiro. Fórmula Standard sem desconto: entrada × preço + (candidatos + pensamentos) × preço, divididos por 1M.
- [ ] **Checkpoint:** revisar saídas simuladas e commit local `Add single-call Gemini diagnosis and safe summaries`, sob a regra de checks de A1.

### A3. Jobs, cópias e dry-run

**Files:** arquivos A nos três repos; `.github/workflows/gemini-pilot.yml`, `.github/pilot.md`.

**Interfaces:** dispatch recebe somente `pr`, `sha`, `dry_run` (default true). `collect` emite apenas snapshot/Decision em outputs; `infer` emite resultado público filtrado; `summarize` consome artefato daquele run. `liveEnabled:false` impede chamada real mesmo com chave presente. Dry-run usa fixture identificada como simulação, nunca como análise real.

- [ ] **Testes vermelhos:** ampliar `pilot.test.mjs` com `workflow_contract` verificando triggers somente dispatch, permissões por job sem write, concorrência por repo/cancel-in-progress false, timeout 10 min, ausência de PR checkout e upload bruto. `dry_run` exige zero requisições Gemini e zero operações GitHub mutantes. `live_disabled` bloqueia mesmo se key sintética existir. `artifact_origin` recusa artifact de outro run, arquivo extra ou path arbitrário.
  Assertions do CLI simulado: `assert.equal(transport.geminiCalls, 0)` em dry-run; `assert.equal(transport.githubWrites, 0)` em todos os modos.
  `anonymous_scripts` exige ausência de actions/checkout/github.token/GH_TOKEN/Authorization no bootstrap da inferência; SHA de workflow confiável, três paths fixos e SHA-256 por arquivo. Hash divergente, HTTP inválido ou SOURCE_SHA igual ao PR head indevidamente escolhido bloqueiam antes da execução/API.
- [ ] **Provar falha:** `node --test .github/scripts/pilot.test.mjs` em Cubeship; contrato do workflow ausente falha.
- [ ] **Criar jobs:** coleta com somente contents/pull-requests/actions/checks read; inferência `permissions:{}`, chave apenas no passo REST; validação/resumo sem chave e apenas leituras necessárias/download do run atual. Coleta/resumo podem obter scripts confiáveis com checkout read-only e `persist-credentials:false`. Inferência não usa checkout: antes do passo com chave, `curl --disable --fail --silent --show-error --proto '=https' --max-time 20` obtém os três arquivos fixos `.github/scripts/pilot-core.mjs`, `pilot-run.mjs`, `pilot-config.json` de `https://raw.githubusercontent.com/{repo-permitido}/{SOURCE_SHA}/{path}`. SOURCE_SHA é o SHA completo do workflow confiável (`github.workflow_sha`), nunca input do dispatch/head do PR; conferir cada SHA-256 literal revisado no YAML antes de executar. Não seguir redirects, usar configuração curl, header Authorization, Git, GH_TOKEN ou github.token nessa rota. Nenhum secret entra no passo de download. Action pins completos verificados no upstream. Dry-run não referencia secret. Copiar scripts/testes/config para templates/Jian; adaptar só branch padrão e docs. Não adicionar chave, mudar liveEnabled, billing ou settings.
- [ ] **Provar passagem:** rodar `node --test .github/scripts/pilot.test.mjs` nos três repos e `cmp` dos quatro arquivos comuns. Validar YAML com ferramenta já disponível; `actionlint` não estava instalado na inspeção, portanto não instalar silenciosamente nem declarar lint executado. Se indisponível, registrar revisão estrutural e reservar validação real de Actions para depois da autorização de publicação.
- [ ] **Checkpoint:** commits locais separados `Add manual Gemini pilot dry-run workflow`; documentar uso, US$ 5/100 análises acompanhamento manual, disable por liveEnabled, visibilidade pública e estados incompleto/obsoleto/incerto. A é entregável offline, ainda não ativado.

## B — CI agregado determinístico, independente de A

### B1. Preservar essenciais e concluir explicitamente

**Files:** três workflows CI existentes; `.github/scripts/ci-aggregate.mjs`, `ci-aggregate.test.mjs` em cada repo.

**Interfaces:** `evaluate(required:string[],needs:Record<string,{result:string}>,optional:Record<string,{applicable:boolean}>={}):{ok:boolean,reasons:string[]}`. CLI recebe JSON via env `NEEDS_JSON` e allowlist fixa do repo, devolve exit 0 apenas com cobertura válida. Listas: Cubeship `check,database,integration,scripts,dashboard,site`; Jian `check,storybook,postgres`; templates `validate`. Smoke manual não vira dependência do CI de PR.

- [ ] **Testes vermelhos:** `essential_matrix`: todos success→ok; cada essencial em failure/cancelled/skipped/missing/pending→false. `conditional_skip`: opcional skipped passa somente com applicable=false explícito; seletor ausente não passa. `unrelated_check`: job verde extra não substitui essencial; entrada malformada e ID inesperado bloqueiam. Executar fixture de cada repo.
  Assertion mínima: `assert.equal(evaluate(['validate'], {validate:{result:'skipped'}}).ok, false)`.
- [ ] **Provar falha:** `node --test .github/scripts/ci-aggregate.test.mjs` em cada repo.
- [ ] **Implementar evaluator e job:** `aggregate`, nome `CI / aggregate`, `needs` literal, `if: always()`, sem checkout e sem secrets. Incorporar evaluator como corpo literal fixo de Node no YAML, com teste que compara esse corpo ao script; evita executar código do PR. `NEEDS_JSON` vem de `toJSON(needs)`, nunca interpolado em shell. Não remover passos, adicionar paths/short ou mudar branches/required checks. Drift extra de Cubeship fica fora deste bloco.
- [ ] **Provar passagem:** mesmos testes exit 0; revisar diff contra snapshot anterior demonstrando que jobs/passos essenciais permanecem. Após autorização de publicação, testar falha/cancelamento real de workflow; job final usa dados do mesmo run/SHA, sem coletar checks externos.
- [ ] **Checkpoint:** commit local por repo `Add deterministic aggregate CI result`, respeitando checks exigidos. B funciona sem API/key Gemini.

## C — Pin e smoke de templates, independente de A/B

### C1. Pinar validador sem mudar sua cobertura

**Files:** templates `.github/workflows/check.yml`; `.github/template-smoke.md` (seção do pin).

**Interfaces:** validador existente `go run ./tools/templatecheck ../..`, dentro de `_cubeship/product`; entrada é repo de templates inteiro. Candidato `8845824cd1eb98290d44a93d9fe088e333ef4e0c`; versão Go obtida de `_cubeship/product/go.mod`, sem inventar atualização.

- [ ] **Preparar prova de compatibilidade:** conferir clones nos SHAs da especificação, revalidar instructions, executar no Mac `go -C ../cubeship/product run ./tools/templatecheck ../../cubeship-templates`. Esperado exit 0, `validated N templates`, N igual às pastas válidas. Não instalar toolchain/dependências sem autorização: se ausentes, registrar blocker, não pinar como compatível.
- [ ] **Alterar somente ref:** substituir master pelo SHA completo candidato; pinar actions tocadas por commits upstream verificados. Não alterar manifests para forçar passagem.
- [ ] **Verificar:** repetir comando com o mesmo SHA, conferir `git diff --check` e diff do workflow. Failure deixa candidato não aprovado; uma atualização do pin exige nova compatibilidade/revisão.
- [ ] **Checkpoint:** commit local `Pin Cubeship template validator`, com documento de evidência do SHA/contagem. Pode entregar C1 sem smoke.

### C2. Seletor e fontes limitadas

**Files:** templates `.github/scripts/template-smoke.mjs`, `template-smoke.test.mjs`, `smoke-source.go`, `smoke-source_test.go`.

**Interfaces:** `selectTargets(changes,complete):{state,targets:('mailpit'|'prometheus')[]}` e `profile(target,imageConfig):{mode:'inspect'|'version',command:string[]}` em Node; `readSource(path string)(Source,error)` em Go, `Source={target,image,tag,dockerfile}`. Helper usa `template.Validate`, retorna somente fonte do app previsto, nunca env/inputs; executar a partir de `_cubeship/product` para usar o módulo existente. CLI exige path regular dentro das duas pastas.

- [ ] **Testes vermelhos:** Node `selection` cobre manifest/Dockerfile/COPY/config, rename/delete, README/ícone, mudança do próprio seletor→ambos, diff incompleto→incomplete, nunca terceiro alvo. Go `TestReadSource` cobre YAML válido, inválido, symlink/traversal, múltiplos apps inesperados e registry/repo diferente. Perfil com Config.Volumes não vazio sempre mode=inspect.
  Assertions: `assert.deepEqual(selectTargets(readmeOnly, true).targets, [])`; `assert.equal(profile('prometheus', {Config:{Volumes:{'/prometheus':{}}}}).mode, 'inspect')`; Go exige erro de `readSource` para fixture inválida.
- [ ] **Provar falha:** `node --test .github/scripts/template-smoke.test.mjs`; no módulo confiável, `go -C ../cubeship/product test ../../cubeship-templates/.github/scripts/smoke-source.go ../../cubeship-templates/.github/scripts/smoke-source_test.go`; esperado contratos ausentes.
- [ ] **Implementar:** allowlist Mailpit `axllent/mailpit`, Prometheus fonte local `prometheus/Dockerfile` com base `prom/prometheus`. Fora disso, incompleto/revisão humana, nunca fetch arbitrário. YAML via pacote Go existente, sem parser regex ou biblioteca nova. Version probe Mailpit `/mailpit --version` somente se imagem permitir usuário não root/read-only/sem volume. Prometheus normalmente declara VOLUME upstream: build/inspeção apenas, sem criar container; confirmar no digest efetivo.
- [ ] **Provar passagem:** mesmos comandos exit 0; mocks verificam ausência de env/secrets na fonte e no profile.
- [ ] **Checkpoint:** commit local `Select two safe template smoke targets`.

### C3. Worker sem secrets e execução limitada

**Files:** templates `template-smoke.mjs`, `template-smoke.test.mjs`, `.github/workflows/template-smoke.yml`, `.github/template-smoke.md`.

**Interfaces:** consumir C2; `runTarget(target,source,exec):Promise<{target,digest,platform,mode,state}>`; exec usa spawn com argv, nunca shell. Dispatch recebe PR/SHA, revalida mantenedor/head/base e coleta diffs públicos; um job de controle read-only sem chave Gemini, worker separado sem secrets e `permissions:{}`.

- [ ] **Testes vermelhos:** `docker_contract` exige digest/plataforma resolvidos, sem login/push/cache privilegiado/compose/volumes/socket/host network. Mailpit sem VOLUME usa probe `--network none --read-only --cap-drop ALL --security-opt no-new-privileges --cpus 1 --memory 512m --pids-limit 128` e usuário não root; 30 s, remover container em finally. Prometheus com VOLUME produz zero create/run. `timeouts` encerra processo/imagem em 10 min; `fork` prova zero secret/token persistido; `changed_head` impede teste de snapshot diferente.
  Assertions no mock exec: `assert.equal(exec.containerCreates, 0)` com VOLUME; `assert.equal(exec.pushes, 0)` para todos os perfis; `assert.equal(exec.cleanupCalls, 1)` após timeout de probe.
- [ ] **Provar falha:** `node --test .github/scripts/template-smoke.test.mjs`, novos testes FAIL.
- [ ] **Implementar worker:** ubuntu hospedado efêmero, máximo dois workers; scripts do SHA confiável, dados/source tree do SHA público do PR sem hooks/submodules/credenciais. Não substituir scripts por checkout do PR. Resolver registry/tag em digest, build local sem push e sem secrets; nunca volumes ou serviços reais. Capturar/redigir saídas Docker, publicar apenas estado/digest/cobertura no resumo. Para source removida/incompatível, incomplete. Não tocar workflow de imagem de Jian nem iniciar produto/DB.
- [ ] **Provar passagem:** mocks Node exit 0; testes Docker reais somente no runner Linux após autorização de publicar/dispatch, nunca fingir que mocks validam imagem. Se Docker no Mac não estiver disponível/compatível, manter essa validação pendente; não iniciar infraestrutura local nova.
- [ ] **Checkpoint:** commit local `Add manual isolated template smoke`, com profiles/cobertura documentados. C não precisa Gemini.

## Fechamento, autorizações e recomendação

Ordem econômica recomendada: **B1 → C1 → A1–A3 → C2–C3**; cada bloco admite revisão/entrega separada. Permanecem locais até Lucas autorizar publicação; sem push/PR automático. Executar gates exigidos por cada repo apenas se suas dependências já estiverem disponíveis/autorizadas e registrar pass/fail/blocked honestamente.

Ativação é outra decisão: autorizar chave/projeto Paid dedicado, billing, limites/alertas do provedor e permissões read necessárias. Após isso, revisar visibilidade de logs/artefatos com leitor comum e público; confirmar endpoint JSON/LOW/countTokens e usageMetadata com dados públicos, em uma execução autorizada, antes de habilitar liveEnabled. Reconfirmar preços oficiais: Flash US$ 0,75/3,75 por 1M até 2026-12-31 e US$ 1,50/7,50 desde 2027-01-01; Lite US$ 0,30/2,50, sem usá-lo agora. A referência separa tokens candidatos/pensamento; não há fundamento para prometer custo máximo por maxOutputTokens. Manter US$ 5/100 análises como acompanhamento manual e pausar se medição incerta.

Depois do dry-run, avaliar **cinco PRs históricos por repo**, mantenedor registra utilidade/falso positivo; não relaxar orçamento ou capacidades. Branch protection, publicação automática, conteúdo privado e novas fases exigem decisões distintas.

**Autorrevisão do plano:** A cobre controle/IA/dados/resumo/limites; B cobre essenciais/skips; C cobre pin/seleção/isolation. Corrigidos: filtro antes de artefato, scripts nunca do fork, VOLUME herdado sem runtime, pensamento separado no custo, evaluator sem checkout de PR, pasta de testes dentro de `.github` e bootstrap anônimo da inferência sem token implícito de checkout. Os cinco Review Focus têm testes nomeados. Não criado serviço/app/ledger, nenhuma dependência nova; valores da spec preservados. Provas são documentais e uma leitura pública anônima por SHA; compatibilidade do pin, lint Actions, imagem real, credenciais e visibilidade continuam validações futuras claramente separadas.

Recomendo **execução Native nesta mesma tarefa**, usando `superpowers:executing-plans`, com uma revisão independente ao final se Lucas escolher esse método. Interfaces são pequenas, os blocos são independentes e evitar contexto novo por tarefa preserva baixo custo. A alternativa Subagent-driven oferece revisão por tarefa com custo maior. Lucas deve revisar este plano e escolher o método antes de executar.

Referências: [GenerateContent/usageMetadata](https://ai.google.dev/api/generate-content), [countTokens](https://ai.google.dev/api/tokens), [pricing](https://ai.google.dev/gemini-api/docs/pricing), [Prometheus Dockerfile upstream](https://raw.githubusercontent.com/prometheus/prometheus/main/Dockerfile). Verificar o digest/tag efetivo na implementação; upstream móvel não é prova da imagem selecionada.
