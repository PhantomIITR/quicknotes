import React from 'react';
import ReactDOM from 'react-dom';
import $ from 'jquery';
import * as action from './action.js';

const allThemes = [
  'light',
  'dark'
];


const allLayouts = [
  'default',
  'grid',
  'barebones'
];

/*
TODO:
 - when this is shown, the rest should be inactive i.e. make it modal
*/

export default class Settings extends React.Component {
  constructor(props, context) {
    super(props, context);
    this.handleCancel = this.handleCancel.bind(this);
    this.handleLayoutChanged = this.handleLayoutChanged.bind(this);
    this.handleOk = this.handleOk.bind(this);
    this.handleThemeChanged = this.handleThemeChanged.bind(this);

    this.state = {
      theme: 'light',
      layout: 'default'
    };
  }

  handleThemeChanged(e) {
    const theme = e.target.value;
    console.log('handleThemeChanged: ', theme);
    this.setState({
      theme: theme
    });
    $('body').removeClass();
    $('body').addClass('theme-' + theme);
  }

  handleLayoutChanged(e) {
    const layout = e.target.value;
    console.log('handleLayoutChanged: ', layout);
    this.setState({
      layout: layout
    });
    $('body').attr('data-spacing', layout);
  }

  renderThemesSelect(themes, selected) {
    const options = themes.map(function(theme) {
      return <option key={ theme }>
               { theme }
             </option>;
    });
    return (
      <select value={ selected } onChange={ this.handleThemeChanged }>
        { options }
      </select>
      );
  }

  renderLayoutsSelect(layouts, selected) {
    const options = layouts.map(function(layout) {
      return <option key={ layout }>
               { layout }
             </option>;
    });
    return (
      <select value={ selected } onChange={ this.handleLayoutChanged }>
        { options }
      </select>
      );
  }

  handleOk(e) {
    e.preventDefault();
    action.hideSettings();
  }

  handleCancel(e) {
    e.preventDefault();
    action.hideSettings();
  }

  render() {
    const layouts = this.renderLayoutsSelect(allLayouts, this.state.layout);
    const themes = this.renderThemesSelect(allThemes, this.state.theme);
    return (
      <div id="settings">
        <div className="settings-div">
          Layout:
          { layouts }
        </div>
        <div className="settings-div">
          Theme:
          { themes }
        </div>
        <div className="settings-buttons">
          <button onClick={ this.handleOk }>
            Ok
          </button>
          <button onClick={ this.handleCancel }>
            Cancel
          </button>
        </div>
      </div>
      );
  }
}
