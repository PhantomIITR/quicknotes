import React, { Component, PropTypes } from 'react';
import marked from 'marked';
import CodeMirrorEditor from './CodeMirrorEditor.jsx';
import * as action from './action.js';
import * as ni from './noteinfo.js';
import { debounce } from './utils.js';

function getWindowMiddle() {
  const dy = window.innerHeight;
  return dy / 2;
}

export default class EditorNew extends Component {
  constructor(props, context) {
    super(props, context);

    this.toggleEditor = this.toggleEditor.bind(this);
    this.toHtml = this.toHtml.bind(this);
    this.editNote = this.editNote.bind(this);
    this.createNewNote = this.createNewNote.bind(this);
    this.handleTextChanged = this.handleTextChanged.bind(this);

    this.top = getWindowMiddle();
    this.state = {
      isShowing: false,
      note: null,
      txt: ''
    };
  }

  componentDidMount() {
    marked.setOptions({
      renderer: new marked.Renderer(),
      gfm: true,
      tables: true,
      breaks: true,
      pedantic: false,
      sanitize: true,
      smartLists: true,
      smartypants: false
    });
    action.onToggleEditor(this.toggleEditor, this);
    action.onEditNote(this.editNote, this);
    action.onCreateNewNote(this.createNewNote, this);
  }

  componentWillUnmount() {
    action.offAllForOwner(this);
  }

  handleTextChanged(e) {
    const s = e.target.value;
    this.setState({
      txt: s
    });
  }

  editNote(note) {
    console.log('EditorNew.editNote: note=', note);
    //this.editAreaNode.editor.focus();
    let s = ni.FetchContent(note, () => {
      this.setState({
        txt: ni.Content(note)
      });
    });
    s = s || "";
    this.setState({
      note: note,
      txt: s,
      isShowing: true
    });
  }

  createNewNote() {
    console.log('EditorNew.createNewNote');
    // TODO: create empty default note with empty id
    //this.editAreaNode.editor.focus();
    this.setState({
      note: null,
      isShowing: true
    });
  }

  toggleEditor() {
    this.top = getWindowMiddle();
    this.setState({
      isShowing: !this.state.isShowing
    });
  }

  toHtml(s) {
    s = s.trim();
    const html = marked(s);
    return html;
  }

  renderMarkdownButtons() {
    console.log('renderMarkdownButtons');
    return (
      <div id="edit2-button-bar" className="flex-row">
        <button className="ebtn no-text" title="Strong (⌘B)">
          <i className="fa fa-bold"></i>
        </button>
        <button className="ebtn no-text" title="Emphasis (⌘I)">
          <i className="fa fa-italic"></i>
        </button>
        <div className="d-editor-spacer"></div>
        <button className="ebtn no-text" title="Hyperlink (⌘K)">
          <i className="fa fa-link"></i>
        </button>
        <button className="ebtn no-text" title="Blockquote (⌘⇧9)">
          <i className="fa fa-quote-right"></i>
        </button>
        <button className="ebtn no-text" title="Preformatted text (⌘⇧C)">
          <i className="fa fa-code"></i>
        </button>
        <button className="ebtn no-text" title="Upload">
            <i className="fa fa-upload"></i>
        </button>
        <div className="d-editor-spacer"></div>
        <button className="ebtn no-text" title="Bulleted List (⌘⇧8)">
          <i className="fa fa-list-ul"></i>
        </button>
        <button className="ebtn no-text" title="Numbered List (⌘⇧7)">
          <i className="fa fa-list-ol"></i>
        </button>
        <button className="ebtn no-text" title="Heading (⌘⌥1)">
          <i className="fa fa-font"></i>
        </button>
        <button className="ebtn no-text" title="Horizontal Rule (⌘⌥R)">
          <i className="fa fa-minus"></i>
        </button>
      </div>
      );
  }

  renderMarkdownPreview() {
    console.log('renderMarkdownPreview: s=', s);

    const mode = 'text';
    const s = this.state.txt;
    const html = {
      __html: this.toHtml(s)
    };

    const setEditArea = el => this.editAreaNode = el;
    const style1 = {
      display: 'inline-block',
      paddingTop: 8
    };

    const style = {
      top: this.top
    };

    return (
      <div id="editor2-wrapper" className="flex-col" style={ style }>
        <div className="drag-bar-vert"></div>
        <div id="editor2-top" className="flex-row">
          <input id="editor2-title" className="editor-input half" placeholder="title goes here...">
          </input>
          <input id="editor2-tags" className="editor-input half" placeholder="#enter #tags">
          </input>
        </div>
        <div id="edit2-text-with-preview" className="flex-row">
          <div id="edit2-preview-with-buttons" className="flex-col">
            { this.renderMarkdownButtons() }
            <CodeMirrorEditor mode={ mode }
              className="edit2-textarea-wrap"
              textAreaClassName="edit2-textarea"
              placeholder="Enter text here..."
              value={ s }
              onChange={ this.handleTextChanged }
              ref={ setEditArea } />
          </div>
          <div id="edit2-preview" dangerouslySetInnerHTML={html}></div>
        </div>
        <div id="edit2-bottom" className="flex-row">
          <div>
            <button className="btn btn-primary">
              Save
            </button>
            <button className="btn btn-primary">
              Discard
            </button>
            <div style={ style1 }>
              <span>Format:</span>
              <span className="drop-down-init">markdown <i className="fa fa-angle-down"></i></span>
            </div>
          </div>
          <div id="editor2-hide-preview">
            <span>hide preview</span>
          </div>
        </div>
      </div>
      );
  }

  render() {
    console.log('EditorNew.render, isShowing:', this.state.isShowing, 'top:', this.top);

    if (!this.state.isShowing) {
      return <div className="hidden"></div>;
    }

    return this.renderMarkdownPreview();
  }
}
