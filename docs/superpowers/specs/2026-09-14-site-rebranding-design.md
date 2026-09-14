# Cubeship — rebranding do site

Status: direção aprovada em conversa em 2026-09-14, incluindo artes geradas para compor o site. Implementação concluída; build de produção concluído. Testes e review dispensados por instrução posterior do usuário.

## Direção recomendada

Uma plataforma de infraestrutura apresentada como um produto de precisão: um
cubo tridimensional que contém e conecta os serviços, uma composição ampla e
tipografia marcante, seguida imediatamente pelo dashboard real. O cyberpunk vem
da luz, da geometria e da materialidade da interface. O profissionalismo vem da
hierarquia, da legibilidade e de mostrar o que o produto realmente faz.

O público é quem desenvolve e opera aplicações e quer uma plataforma nos próprios
servidores. A conversão principal é começar a instalação; explorar o produto,
consultar a documentação e abrir o código são caminhos secundários. O conteúdo
continua em inglês, como o site atual.

## Alternativas consideradas

| | Produto cinematográfico — recomendada | Console editorial | Experiência 3D contínua |
| --- | --- | --- | --- |
| Elemento central | Cubo 3D no hero e dashboard em destaque | Dashboard, diagramas e tipografia | Uma cena que acompanha toda a navegação |
| Movimento | Abertura do cubo e transições ligadas ao scroll | Transições leves em CSS | Câmera e objetos coreografados por seção |
| Identidade | Cyberpunk com presença de produto | Cyberpunk mais técnico e contido | Cyberpunk mais experimental |
| Compromisso | Cena delimitada, com fallback estático | Menor impacto visual tridimensional | Mais custo de GPU e complexidade no mobile |

## O que a leitura do site mostrou

- A home tem nove blocos principais. Uma grade de quinze funcionalidades aparece
  antes das capturas do produto, adiando a demonstração visual.
- O hero já tem um shader WebGL, via OGL, com glifos e efeitos de terminal.
  A proposta substitui esse fundo pelo objeto tridimensional da marca.
- Existem capturas reais de overview, app e backups, com dados de demonstração.
  Elas podem sustentar a apresentação sem inventar uma interface.
- Home, docs, templates e comparativos compartilham os tokens da paleta e
  componentes de navegação; precisam formar uma experiência coerente.
- O catálogo é externo ao processo do site. A home continua utilizável quando
  ele está indisponível, e as páginas de templates já têm limites e erros próprios.

## Sistema visual

### Paleta e materiais

Manter a família cromática do produto: fundo #05070a, superfície #0a0e14,
estrutura #182430, texto #d7e3ec, ciano #2de2e6 e magenta #ff2e88.
Magenta aparece como uma luz secundária pontual no objeto 3D; ciano identifica
ações e conexões. A leitura permanece em superfícies escuras estáveis.

O cubo tem faces escuras translúcidas, arestas iluminadas e um núcleo composto
por módulos de infraestrutura. A iluminação produz volume sem espalhar glow
por todo texto e botão. Linhas e cantos retos continuam reconhecíveis na marca.

### Tipografia

Chakra Petch continua como assinatura dos títulos, com escala e espaçamento
mais expressivos. JetBrains Mono fica reservada a comandos e dados técnicos.
Texto corrido ganha altura de linha e largura controlada; rótulos em maiúsculas
passam a ser excepcionais. As fontes continuam locais.

### Composição

Conteúdo alinhado à esquerda, áreas de respiro generosas e larguras diferentes
para demonstração, texto e navegação. Alternar cenas amplas com seções concisas,
em vez de repetir uma grade de cards para cada funcionalidade.

```text
Marca          Produto · Templates · Docs · GitHub       Install Cubeship

Your infrastructure.                    Cubo de serviços
Ready to ship.                          iluminado em 3D
Promessa clara + ações                  abertura ligada ao scroll

            Dashboard real em uma área ampla
            Overview / Deployments / Backups

Do código à aplicação                   Visual do fluxo de deploy

Apps, dados e proteção                  Recursos agrupados por uso

Um servidor → um cluster                Diagrama de expansão

Dashboard · CLI · API · MCP              Exemplo de operação

Templates disponíveis                   Catálogo real

Seu próximo deploy começa aqui          Comando de instalação
Rodapé completo
```

## Landing page

1. **Primeira dobra:** headline “Your infrastructure. Ready to ship.”,
   descrição “Deploy apps, run databases and manage your servers with one
   self-hosted platform.”, CTA “Install Cubeship” e ação “Explore the platform”.
   O cubo ocupa a outra metade da composição. A mensagem e as ações são HTML.
2. **Produto em primeiro plano:** logo depois do hero, uma captura ampla do
   dashboard com seleção acessível entre overview, app e backups. Legendas
   descrevem o que a pessoa consegue observar e controlar.
3. **Deploy:** demonstrar os caminhos reais — imagem publicada, registry próprio
   e repositório GitHub — em uma composição de fluxo com exemplos curtos.
4. **Plataforma:** substituir a sequência de quinze cards equivalentes por
   grupos de aplicações, dados e operação. Dar contexto a bancos, backups,
   storage, certificados, DNS, firewall e métricas.
5. **Cluster:** mostrar um servidor se conectando a outros. A animação explica
   distribuição de aplicações e entrada centralizada, sem sugerir uma
   arquitetura ou alta disponibilidade que o produto não entrega.
6. **Operação por agentes:** apresentar dashboard, CLI, API e MCP como caminhos
   para o mesmo produto. Exemplos e limites devem corresponder à implementação.
7. **Templates:** vitrine com dados reais do catálogo e acesso claro à busca.
   Se o catálogo falhar, a home continua íntegra.
8. **Fechamento:** propriedade da infraestrutura e licença, comando copiável,
   link para os requisitos e CTA de instalação. Nenhuma métrica, cliente,
   depoimento ou promessa de desempenho será fabricada.

## Three.js e movimento

Recomendo Three.js para construir a geometria do cubo e seus módulos, substituindo
o shader decorativo atual. A cena pertence a um componente cliente isolado e
carregado sob demanda; o restante da página continua renderizado no servidor.

- Uma cena WebGL na home, com geometria procedural e sem modelos remotos.
- Uma entrada breve revela o objeto; o scroll separa seus módulos e conduz
  visualmente ao dashboard. O scroll mantém seu comportamento nativo.
- Interação de ponteiro discreta em dispositivos com ponteiro preciso.
- A cena deixa de renderizar quando sai da viewport ou a aba fica oculta.
- Resolução de renderização limitada; resize acompanha o contêiner.
- Redução de movimento desativa a coreografia. Sem WebGL, erro de inicialização
  ou perda de contexto, aparece uma composição estática do mesmo cubo.
- No mobile, a geometria e a composição se simplificam, e o texto e os CTAs
  mantêm prioridade. A imagem estática fica disponível desde o HTML inicial.
- Recursos da GPU, observers e listeners são liberados ao desmontar a cena.
- As demais animações usam transforms e opacity; apenas os momentos que
  explicam uma mudança recebem movimento, com conteúdo legível sem animação.

As orientações oficiais de [renderização sob demanda](https://threejs.org/manual/en/rendering-on-demand.html)
e [dimensionamento responsivo](https://threejs.org/manual/en/responsive.html)
fundamentam o controle do trabalho da GPU. Fluidez e tamanho final do bundle
serão medidos na implementação; não há benchmark executado nesta proposta.

## Outras páginas

### Documentação

Repaginar o ambiente de leitura: cabeçalho com a marca atualizada, sidebar mais
legível, destaque consistente da página ativa, escala tipográfica, espaçamento,
blocos de código e callouts. Manter busca, índice, navegação mobile, cópia em
Markdown e URLs existentes. A documentação usa a mesma identidade com uma
densidade apropriada à leitura; a cena 3D fica na landing.

### Templates

Dar à listagem uma abertura de catálogo de produto, busca destacada e filtros
claros. Cards priorizam ícone, nome e descrição. No detalhe, organizar README,
recursos criados e metadados da release em áreas reconhecíveis, com tratamento
responsivo. Revisar também resultados vazios e erro do catálogo.

### Comparativos e compartilhamento

Aplicar a identidade às páginas de comparação, mantendo tabelas semânticas e
leitura mobile. Preservar datas e ressalvas das comparações existentes; afirmações
novas sobre concorrentes exigem verificação. Atualizar imagens de compartilhamento
da home e o tratamento visual dos geradores de imagens de docs e templates.

## Arquitetura e limites

- Trabalhar em `master`, na raiz do repositório, conforme AGENTS.md.
- Concentrar as alterações em `hosted/site/`; atualizar a documentação de design
  do site para refletir a nova composição e o ciclo de vida da animação.
- Preservar a paleta base comum ao dashboard. Novos estilos de marketing ficam
  restritos ao site, sem exigir uma reforma do dashboard.
- Manter Fumadocs para documentação e suas funcionalidades; a navegação de
  marketing pode ganhar apresentação própria, com links compartilhados.
- Preservar rotas, SEO, instalador, proxy do catálogo, busca e analytics.
- A home mantém o prazo de 800 ms e a degradação atual da faixa de templates.
- Remover OGL e o componente antigo somente se não houver outros consumidores.
- Não publicar nem criar um commit de implementação como parte da aprovação
  desta proposta. O primeiro resultado executável será um preview local.

## Critérios de conclusão da implementação

- Home visualmente revista em desktop e mobile, com a cena 3D funcionando e
  fallback verificado; capturas reais do produto aparecem antes da lista extensa
  de funcionalidades.
- Docs, templates, detalhe de template, comparativos e imagens de compartilhamento
  seguem a identidade, cada um com densidade adequada à sua função.
- Navegação por teclado, foco visível, menu mobile, tabs, busca, cópia do comando
  e preferência por movimento reduzido funcionam.
- Conferir layouts em 390, 768 e 1440 pixels de largura; sem overflow da página.
- Validar a home com catálogo indisponível, sem WebGL e com movimento reduzido.
- Executar Biome, testes do site, typecheck e build; executar `make check`.
- Revisar erros de console, carregamento da cena e atividade fora da viewport,
  registrando limitações que não puderem ser verificadas no ambiente.

## Decisão solicitada

Aprovar a direção **produto cinematográfico**: cubo 3D como assinatura visual,
dashboard em primeiro plano, narrativa de produto ao scroll e aplicação da
identidade a docs, templates e comparativos. A aprovação autoriza detalhar o
plano técnico e implementar o conjunto em preview local.
