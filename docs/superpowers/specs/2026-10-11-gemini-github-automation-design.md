# Piloto Gemini em GitHub Actions

2026-10-11 — desenho aprovado por Lucas às 21:17 UTC. Este documento registra a especificação aprovada; o estado de implementação será documentado em `.github/pilot.md`. Na entrega inicial do desenho, nenhum workflow, dependência, secret, billing ou permissão havia sido configurado.

## Escopo e estado observado

Diagnóstico consultivo de PR em `cubeshipd/cubeship`, `cubeshipd/cubeship-templates` e `lucasaarch/jian`, iniciado manualmente por mantenedor, com saída apenas no resumo do run. Sem comentários ou checks de IA escritos no PR, aprovação, merge, push ou correção automática. Labels, tradução e deduplicação de issues ficam para depois. Não requer ledger, app próprio ou infraestrutura externa.

Snapshots públicos inspecionados; instruções/CI lidos e trabalho existente preservado:

| Repositório / SHA | CI essencial observado |
| --- | --- |
| Cubeship / `8845824cd1eb98290d44a93d9fe088e333ef4e0c` | `check`, `database`, `integration`, `scripts`, `dashboard`, `site` |
| Templates / `7156f25dc6173e97f3bf5b2b0ac4f0d54e15119e` | `validate`, usando validador do `master` móvel de Cubeship |
| Jian / `77b8fc4f87913463c73e29c2def9cfce9800f215` | `check`, `storybook`, `postgres`, incluindo Docker build |

Sem `.agents/skills` nesses snapshots. Jian `image.yml` publica imagens: excluído do smoke.

## Arquitetura pequena

Um workflow manual por repo, com jobs separados e scripts/actions confiáveis pinados, nunca vindos do PR:

1. **Coleta:** validar branch padrão, ator original/de rerun com acesso atual `maintain`/`admin`, PR aberto e SHA esperado. GitHub API fornece head/base SHA, diff público limitado e metadados do CI; sem executar PR ou baixar logs. Token somente de leitura (`contents`, `pull-requests`, `actions`, `checks`); passar entre jobs apenas IDs/SHAs.
2. **Inferência:** buscar novamente o diff público daquele snapshot e filtrá-lo antes da chamada. Chave Gemini somente no passo de API; nenhum token GitHub nesse passo. Usar uma chamada REST GenerateContent com `responseMimeType=application/json`, JSON schema, um candidato e thinking `low`; sem ferramentas, execução de código, grounding, URLs externas, conversa, cache explícito ou Interactions. Modelo sem capacidades de escrita. Dados temporários locais são removidos; nunca upload do prompt/diff/resposta bruta.
3. **Validação e resumo:** receber por artefato nativo de um dia apenas JSON pequeno previamente filtrado, sem código/logs/secrets, conferindo run/SHAs/origem. Validar esquema fechado, até três achados com severidade enumerada, path/linha existente no diff, cobertura e resumo. Rejeitar extras, truncamento e referências inválidas. Renderer confiável sanitiza Markdown/HTML/links e escreve somente o resumo do run, sem credenciais de escrita no repo. Achados continuam hipóteses para revisão humana.

Reconsultar head/base antes da chamada/resumo; mudança encerra como obsoleto. Exibir sempre SHAs analisados: permanece corrida residual entre leitura e resumo, sem alegar análise do HEAD atual.

**Idempotência best-effort:** identificar repo/PR/SHA no `run-name`, consultar histórico paginado e recusar SHA já iniciado e geração em rerun. `concurrency` serializa por repo sem cancelar run ativo. Histórico indisponível bloqueia; remoções/corridas ainda podem duplicar chamada. Sem garantia de exatamente uma chamada. Timeout/resultado incerto exige decisão humana, nunca retry automático.

## Dados e segurança

Somente conteúdo público. Excluir `.env*`, chaves, dumps, binários e paths sensíveis; scanner de secrets bloqueia suspeitas. Sem logs brutos de CI. Diff/título/paths são dados não confiáveis: não interpolar em shell, seguir instruções/links ou executar PR. Ausência de capacidades limita injection; schema não garante veracidade.

Logs/resumo/artefatos são visíveis a leitores do repo; acesso de leitura basta para ver/baixar logs, inclusive públicos. Testar acesso público/leitor comum e permissões mínimas antes da ativação. Registrar apenas IDs, estados, contagens, latência e tokens; nunca payload, cabeçalhos, chave, pensamentos ou resposta bruta. Artefato somente com resultado público sanitizado; masking não substitui filtragem. Código privado excluído até autorização de envio/acesso/retenção; exige Paid, sem fallback Free ou promessa de retenção zero.

## Mudanças determinísticas separadas

**CI agregado:** mudança própria, independente de Gemini, preservando todos os jobs da tabela. Job final com `needs` explícito e `if: always()`, sem checkout, avalia conclusões. Falha, cancelamento, ausência e skip essencial inesperado nunca passam. Skip de job condicional exige seletor confiável comprovando não aplicabilidade; nenhum job essencial atual ganha skip por paths. Checks/artefatos de outro SHA não são evidência. IA indisponível não altera CI. Branch protection não muda. Decidir à parte os targets de drift `changelog-check`/`reference-check` de Cubeship, sem chamada explícita no CI atual.

**Pin do validador:** mudança própria em templates, usando SHA completo revisado de Cubeship; candidato é o snapshot acima, condicionado à validação de todas as pastas. Atualizar o pin por revisão, não acompanhar `master` automaticamente.

**Smoke inicial:** manual e separado da chave Gemini, para no máximo dois templates simples: Mailpit e Prometheus, sujeitos à verificação do perfil. Selecionar por diff de manifest/Dockerfile/COPY/configuração; README/ícone isolados não exigem imagem. Resolver tags para digests e usar comandos fixos aprovados, como versão/configuração, sem instalar templates. Build de fork apenas em runner hospedado efêmero, sem secrets, escrita, credenciais persistidas ou cache privilegiado; nunca em `pull_request_target`. Container sem volumes, serviços reais, socket do host ou portas públicas; probe sem rede, read-only e limites. `VOLUME` automático/dependência de serviços limita o alvo a inspeção, sem executar container. Diff incompleto não autoriza skip. Preservar builds atuais de Cubeship/Jian; não iniciar daemon/gateway, banco ou provedor.

## Limites e orçamento

| Controle | Proposta inicial |
| --- | --- |
| Entrada | 20 arquivos, 1.000 linhas modificadas, 128 KiB e 12.000 tokens totais; excedeu: incompleto, sem dividir em chamadas |
| Saída | `maxOutputTokens=4096`, JSON até 8 KiB, três achados e resumo até 4.000 caracteres; confirmar contabilização de thinking no plano |
| Execução | Uma tentativa Gemini/run, timeout 90 s, sem retry de geração; leituras GitHub até dois retries com backoff; workflow até 10 min |
| Concorrência/smoke | Um run de diagnóstico/repo; smoke máximo dois alvos, 10 min/imagem e probe 30 s, 1 CPU/512 MiB/128 PIDs |
| Gasto | Meta conjunta US$ 5/mês; começar com até 100 análises/mês acompanhadas manualmente, sem quota global garantida |

Modelo configurável em configuração confiável: `gemini-3.8-flash`, thinking `low`; `gemini-3.5-flash-lite` reservado à futura triagem. Preços oficiais Standard Paid confirmados em 2026-10-11, por milhão de tokens: Flash US$ 0,75 entrada/3,75 saída até dezembro; US$ 1,50/7,50 em janeiro de 2027. Lite US$ 0,30/2,50. Pensamento é faturado. Com 12k entrada e 4.096 saída faturada, estimativa Flash US$ 0,02436 por tentativa hoje, sem garantia de custo efetivo.

Projeto API dedicado, acompanhamento de usageMetadata/fatura, alerta e limite disponível do provedor exigem autorização posterior. Medição ausente/incerta pede pausa e conciliação manual. US$ 5 é orçamento proposto, não hard cap: atrasos, duplicações, preços, limites do provedor, impostos e Actions podem excedê-lo. Reconfirmar preços/semântica de tokens antes do plano e ativação.

## Aceite, etapas e decisões

Testar antes de ativar: ator/ref/SHA inválido; injection/secret/path malicioso; JSON inválido/truncado; HEAD/base mudando; dispatch duplicado/rerun/timeout sem retry; visibilidade dos logs/artefatos; matriz de skips/falhas do agregado; seleção de imagens e ausência de secrets/volumes/push. Fazer dry-run sem IA, depois cinco PRs históricos/repo, avaliados pelo mantenedor. Nenhum teste de produto foi executado nesta entrega.

Lucas revisa o desenho; somente depois escrever plano. Escolhas necessárias: aceitar GenerateContent e saída no resumo; confirmar US$ 5/mês e responsável pelo projeto Paid; aprovar dois perfis de smoke e pin compatível. Chave, billing, permissões mínimas, branch protection e futura publicação automática são autorizações separadas; não há decisão de infraestrutura externa. Autorrevisão conferiu escopo reduzido, dados públicos, separação de jobs, idempotência sem garantia absoluta e orçamento sem hard cap.

Fontes oficiais: [GenerateContent/JSON schema](https://ai.google.dev/api/generate-content), [preços](https://ai.google.dev/gemini-api/docs/pricing), [termos Paid](https://ai.google.dev/gemini-api/terms), [leitura de logs](https://docs.github.com/en/actions/how-tos/monitor-workflows/use-workflow-run-logs), [segurança Actions](https://docs.github.com/en/actions/reference/security/secure-use).
