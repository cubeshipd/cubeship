# Piloto Gemini para triagem de Issues — 12 de outubro de 2026

## Escopo e estado

O escopo corrigido por Lucas é triagem automática de **Issues**: labels, tipo e prioridade nativos, tradução e possíveis duplicatas. Não inclui revisão de PR com IA. Começar por `cubeshipd/cubeship`; replicar para `cubeshipd/cubeship-templates` e `lucasaarch/jian` somente depois de um teste real ponta a ponta.

Implementação e testes locais estão autorizados. `enabled=false` e `publishEnabled=false` permanecem como padrão. Não ativar, enviar dados ao Gemini, comentar, criar Issues de teste ou publicar alterações antes de uma confirmação única de ativação persistente, uso da chave existente e teste externo. As ações de triagem em novas Issues já estão autorizadas; essa autorização não será solicitada novamente. PR52 permanece draft e sem merge; o branch local parte de `master` em `3084c3f`.

## Arquitetura

Disparar somente em `issues.opened`, com opção manual de dry-run por mantenedor. Não escutar edição, comentários, labels, tipo ou campos inicialmente. Executar exclusivamente scripts confiáveis do branch padrão, vinculados ao SHA imutável do workflow e a hashes verificados; conteúdo de Issue nunca vira código, comando, ferramenta ou URL a visitar.

Separar três estágios: **coleta confiável → inferência → publicação controlada**. O estágio Gemini não tem token GitHub, permissão de escrita ou ferramentas. O publicador não recebe a chave Gemini. Uma chamada combinada produz uma proposta estruturada; esquema fechado e validação determinística precedem qualquer escrita.

Recusar repositório privado, PR apresentado como Issue, alvo fechado ou removido, bots, contexto inválido, reruns e entrada sensível. Google recebe somente título e prosa pública sanitizada, mais títulos e pequenos trechos de até dez candidatos públicos do mesmo repositório. Não recebe autores, perfis, comentários, valores de campos ou Projects, código, logs, anexos ou credenciais. Ler no máximo 2 MiB de fonte bruta e detectar credenciais antes de remover blocos; recusar blocos malformados e excesso de prosa, sem truncamento oculto. Corpo composto apenas de bloco omitido pode ficar vazio na entrada, se o título tiver prosa informativa; tradução de corpo vazio não deve inventar conteúdo. O limite de 4.096 bytes se aplica à prosa principal sanitizada, preservando o fingerprint do original completo. A filtragem conservadora não garante detectar todo segredo presente em texto público. Não usar fallback privado ou gratuito.

Vincular a análise ao SHA256 de `título + NUL + corpo original`. Reconsultar conteúdo e estado antes da geração e de cada etapa de publicação; mudanças bloqueiam escritas obsoletas. Serializar o workflow inteiro em um grupo fixo por repositório, com `cancel-in-progress: false` e `queue: max`, antes da leitura de histórico/admissão; isso também serializa cada Issue. A fila oficial comporta até cem pendentes; excedentes são cancelados, sem garantia de atendimento ilimitado. Primeira tentativa e histórico persistente de Actions reservam qualquer geração iniciada, inclusive com falha ou timeout. O coletor confiável faz a admissão pelo histórico completo e atesta o resultado em artefato do mesmo run, sob serialização global de todo o workflow. Distinguir geração iniciada, dry-run e etapas puladas; lacunas/ambiguidade e outra execução ativa bloqueiam admissão. A inferência verifica origem do próprio run e snapshots públicos atuais, sem reler todo o histórico anonimamente. Histórico indisponível bloqueia. Não repetir geração ou mutações cegamente. Comentário útil usa marcador estável do fingerprint e somente comentário pertencente ao bot. A idempotência é de melhor esforço, sem promessa de transação atômica ou cobrança exatamente uma vez.

## Metadados reais e efeitos permitidos

Catálogo verificado com acesso de leitura existente:

| Campo | Valores nativos |
| --- | --- |
| Tipo | Task `858946828`; Bug `858946829`; Feature `858946830` |
| Priority, single-select `46483201` | Urgent `81364764`; High `81364765`; Medium `81364766`; Low `81364767` |

Issue12 já possui Bug, Priority High e Effort Low. **Priority é campo nativo de Issue, sem dependência de Projects.** Escrever prioridade com `POST issues/N/issue-field-values`, uma lista não vazia contendo somente `field_id: 46483201` e o **nome** da opção. Nunca usar PUT, limpar ou substituir todos os campos. Preservar Effort `46483204` e os demais campos. Usar catálogo verificado no código e API `2026-03-10`; confirmar o resultado real e reportar escrita não confirmada sem ampliar permissões, criar campos ou alterar tipos.

Preencher tipo e Priority somente se ausentes e com confiança ≥0,85. Preservar metadados humanos, labels existentes e título/corpo originais. Não converter P1/P2/P3: uma label de prioridade legada impede inferência de Priority enquanto não houver migração humana. Adicionar no máximo três labels existentes da allowlist: `bug`, `enhancement`, `documentation`, `question`, `accessibility`, `area: app`, `dependencies`. Não aplicar automaticamente labels de política como duplicate, invalid ou wontfix. O modelo não escolhe IDs de campos.

Sugerir no máximo três possíveis duplicatas, com confiança ≥0,90. Recusar PR, a própria Issue, IDs de outro repositório e candidatos não coletados; verificar novamente IDs e conteúdo antes de publicar e construir links localmente. Nunca fechar, reabrir, atribuir ou excluir Issues.

Traduzir somente em comentário do bot, como texto escapado, mantendo o original. Inglês é o destino provisório enquanto a preferência estiver pendente. Criar comentário apenas se tradução ou duplicatas acrescentarem informação; reutilizar somente marcador do próprio bot. Recusar URLs arbitrárias, HTML inseguro, segredos e campos inesperados gerados pelo modelo. Comandos comuns citados em prosa técnica são dados inertes, não motivo de recusa da Issue; nunca executá-los. Blocos completos de código/logs podem ser omitidos do envio de forma documentada, preservando sempre o corpo original no GitHub.

## Modelo, limites e gasto

Modelo configurável: `gemini-3.5-flash-lite`, sem ferramentas nem exposição de pensamentos. Preço Standard confirmado em 12/10/2026: US$0,30 por milhão de tokens de entrada e US$2,50 de saída, incluindo thinking. [Documentação oficial de preços](https://ai.google.dev/gemini-api/docs/pricing).

| Limite | Valor |
| --- | --- |
| Entrada completa, incluindo sistema e esquema | 32.768 bytes UTF-8 e 8.000 tokens contados |
| Prosa principal / trecho por candidato | 4.096 / 400 bytes |
| Saída | 2.048 tokens e 16.384 bytes JSON |
| Comentário renderizado | 6.000 caracteres |
| Timeout countTokens / geração | 30 / 60 segundos |
| Tentativas de geração e mutações GitHub | Uma, sem retry cego |
| GET GitHub | Até três tentativas, backoff limitado |
| Admissão de gerações iniciadas | Melhor esforço: dez/hora e cem/mês |

Bloquear com histórico ou orçamento desconhecido. Meta mensal: US$5; não é um teto financeiro absoluto, nem limita outras utilizações da conta. A estimativa máxima pelos limites e preço Standard é US$0,00752 por tentativa. Uso real e compatibilidade com o provedor somente serão confirmados após teste autorizado.

## Testes, aceite e etapas

1. Implementar núcleo, transporte e workflow localmente, com ambos os flags desligados; revisão independente e `make check` antes de cada commit.
2. Remover localmente workflow, scripts e guia da antiga IA de PR. Marcar documentos anteriores como históricos; manter o gate agregado e os seis jobs essenciais de CI. Não alterar regras de branch ou misturar release/RC/stable.
3. Apresentar uma confirmação única somente para ativação persistente em `cubeshipd/cubeship`, uso do secret existente e teste externo. Informar os dados enviados ao Google, modelo/custo e capacidade automática futura em `opened`; escritas de tipo/Priority/labels/comentários em novas Issues já estão autorizadas. Propor quatro Issues sintéticas públicas: “Falha simulada ao salvar”, “Synthetic save failure duplicate”, “Melhoria simulada de filtro” e “Synthetic security refusal”. Nenhuma foi criada; seus números só existirão após criação autorizada. Cobrir português, duplicata, classificação, metadados humanos preservados e recusa de segurança.
4. Após autorização, provar o fluxo real no Cubeship antes de replicar. Propor limpeza recuperável: fechar somente as quatro Issues sintéticas e PR52 obsoleto, preservando branches, comentários e histórico para reabertura; não excluir nada nem fazer merge de PR52. Não executar limpeza remota agora.

Testes locais devem cobrir preservação de título/corpo/Effort e metadados humanos, payload aditivo não vazio, catálogo, allowlist, injeção e entrada sensível, quotas e reruns, corrida de snapshots, limites de saída, descarte de assinatura opaca, IDs de duplicatas, links fixos, ausência de execução e segredos em logs/artefatos, e CLI/publicador reais sobre transporte sintético. Bootstrap do workflow deve executar em teste de shell e recusar hash incorreto antes de importar código ou ler chave.

Aceite offline: testes significativos, análise estática e revisão independente aprovados, sem chamada real ou escrita externa e com CI essencial preservado. Aceite real posterior: tradução útil, tipo/Priority nativos confirmados, duplicatas verificadas, originais e Effort intactos, repetição sem novo comentário e consumo real registrado; casos sensíveis, PR, bots e conteúdo alterado devem falhar com segurança.

## Decisões pendentes

Destino da tradução; alvos públicos do E2E; confirmação única de ativação persistente/uso da chave/teste externo e limpeza recuperável; replicação após o E2E. Não configurar credenciais, cobrança, permissões, campos, labels ou Projects nesta preparação.

Contratos oficiais: [valores de campos de Issue](https://docs.github.com/en/rest/issues/issue-field-values), [tipos do repositório](https://docs.github.com/en/rest/repos/issue-types), [catálogo de campos](https://docs.github.com/en/rest/orgs/issue-fields), [eventos de Issues](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#issues).

Concorrência: [contrato oficial e fila](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency). O actionlint local v1.7.12 não reconhece `queue`; validar especificamente essa estrutura contra a documentação e registrar a limitação, sem instalar ferramenta ou ignorar outros erros.
